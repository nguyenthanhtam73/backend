package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserRepository persists and loads users. Implementations live in this package;
// usecases depend on this interface (ports) for testability.
type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	UsernameExists(ctx context.Context, username string) (bool, error)
}

// GormUserRepository is the Postgres-backed UserRepository.
type GormUserRepository struct {
	db *gorm.DB
}

// NewUserRepository returns a UserRepository backed by GORM.
// Callers must pass a non-nil *gorm.DB; methods return errors if db is nil.
func NewUserRepository(db *gorm.DB) *GormUserRepository {
	return &GormUserRepository{db: db}
}

func (r *GormUserRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts a new user row.
func (r *GormUserRepository) Create(ctx context.Context, user *domain.User) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(user).Error
}

// GetByEmail loads a user by case-insensitive email match.
func (r *GormUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	email = strings.TrimSpace(strings.ToLower(email))
	var u domain.User
	tx := db.WithContext(ctx).Where("LOWER(email) = ?", email).First(&u)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &u, nil
}

// GetByID loads a user by primary key.
func (r *GormUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var u domain.User
	tx := db.WithContext(ctx).Where("id = ?", id).First(&u)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &u, nil
}

// UsernameExists returns true if any user has the exact username (case-sensitive as stored).
func (r *GormUserRepository) UsernameExists(ctx context.Context, username string) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	var count int64
	tx := db.WithContext(ctx).Model(&domain.User{}).Where("username = ?", username).Count(&count)
	if tx.Error != nil {
		return false, tx.Error
	}
	return count > 0, nil
}

// AdminUserSearchFilter controls GET /admin/users pagination + query.
type AdminUserSearchFilter struct {
	Query    string
	Page     int
	PageSize int
}

// SearchAdmin lists users matching email / username / display_name (ILIKE).
func (r *GormUserRepository) SearchAdmin(
	ctx context.Context,
	filter AdminUserSearchFilter,
) ([]domain.User, int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, 0, err
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	q := db.WithContext(ctx).Model(&domain.User{})
	term := strings.TrimSpace(filter.Query)
	if term != "" {
		like := "%" + strings.ToLower(term) + "%"
		q = q.Where(
			"LOWER(email) LIKE ? OR LOWER(username) LIKE ? OR LOWER(display_name) LIKE ?",
			like, like, like,
		)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []domain.User
	err = q.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// GetByIDForUpdateTx loads a user with SELECT … FOR UPDATE (must be inside a tx).
// Used by SePay IPN so concurrent webhooks cannot race plan_tier / plan_expires_at.
func (r *GormUserRepository) GetByIDForUpdateTx(tx *gorm.DB, userID uuid.UUID) (*domain.User, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction required")
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	var u domain.User
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", userID).
		First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// UpdatePlanTierTx sets plan_tier (+ expiry) inside an open transaction.
//
//   - Free → always clears plan_expires_at
//   - Paid → writes expiresAt (nil = lifetime / admin grant, SQL NULL)
func (r *GormUserRepository) UpdatePlanTierTx(
	tx *gorm.DB,
	userID uuid.UUID,
	tier domain.PlanTier,
	expiresAt *time.Time,
) (*domain.User, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction required")
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	tier = domain.NormalizePlanTier(tier)
	updates := map[string]any{
		"plan_tier": tier,
	}
	if !tier.IsPaidPlan() || expiresAt == nil {
		// GORM map Updates skips nil — force SQL NULL for Free / lifetime grants.
		updates["plan_expires_at"] = gorm.Expr("NULL")
	} else {
		updates["plan_expires_at"] = *expiresAt
	}
	res := tx.Model(&domain.User{}).
		Where("id = ?", userID).
		Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	var u domain.User
	if err := tx.Where("id = ?", userID).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// ListExpiredPaidUsers returns paid users whose plan_expires_at is at or before now.
// Used by the daily downgrade cron (batch size capped for safety).
func (r *GormUserRepository) ListExpiredPaidUsers(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]domain.User, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	var rows []domain.User
	err = db.WithContext(ctx).
		Where("plan_tier IN ? AND plan_expires_at IS NOT NULL AND plan_expires_at <= ?",
			[]domain.PlanTier{domain.PlanPremium, domain.PlanPremiumPlus},
			now.UTC(),
		).
		Order("plan_expires_at ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}
