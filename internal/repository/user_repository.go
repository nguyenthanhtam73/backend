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

// SubscriptionStatePatch is a partial update for users.* subscription columns.
// Pointer / bool flags distinguish "leave unchanged" from "set NULL".
type SubscriptionStatePatch struct {
	PlanTier           domain.PlanTier
	SetPlanTier        bool
	PlanExpiresAt      *time.Time
	SetPlanExpiresAt   bool // when true, nil PlanExpiresAt → SQL NULL
	TrialEndsAt        *time.Time
	SetTrialEndsAt     bool
	CanceledAt         *time.Time
	SetCanceledAt      bool
	SubscriptionStatus domain.SubscriptionStatus
	SetStatus          bool
}

// UpdatePlanTierTx sets plan_tier (+ expiry) inside an open transaction.
//
//   - Free → clears plan_expires_at + canceled_at; status → expired (keeps trial_ends_at)
//   - Paid → writes expiresAt (nil = lifetime / admin grant, SQL NULL)
func (r *GormUserRepository) UpdatePlanTierTx(
	tx *gorm.DB,
	userID uuid.UUID,
	tier domain.PlanTier,
	expiresAt *time.Time,
) (*domain.User, error) {
	tier = domain.NormalizePlanTier(tier)
	patch := SubscriptionStatePatch{
		PlanTier:         tier,
		SetPlanTier:      true,
		PlanExpiresAt:    expiresAt,
		SetPlanExpiresAt: true,
	}
	if !tier.IsPaidPlan() {
		patch.PlanExpiresAt = nil
		patch.CanceledAt = nil
		patch.SetCanceledAt = true
		patch.SubscriptionStatus = domain.SubStatusExpired
		patch.SetStatus = true
	} else if expiresAt != nil {
		// Best-effort status for legacy callers (SePay fulfill / admin) that
		// have not yet migrated to SubscriptionService.HandleRenewal.
		patch.SubscriptionStatus = domain.SubStatusActive
		patch.SetStatus = true
		patch.CanceledAt = nil
		patch.SetCanceledAt = true
	} else {
		patch.SubscriptionStatus = domain.SubStatusActive
		patch.SetStatus = true
	}
	return r.ApplySubscriptionStateTx(tx, userID, patch)
}

// ApplySubscriptionStateTx updates subscription columns inside an open transaction.
func (r *GormUserRepository) ApplySubscriptionStateTx(
	tx *gorm.DB,
	userID uuid.UUID,
	patch SubscriptionStatePatch,
) (*domain.User, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction required")
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	updates := map[string]any{}
	if patch.SetPlanTier {
		updates["plan_tier"] = domain.NormalizePlanTier(patch.PlanTier)
	}
	if patch.SetPlanExpiresAt {
		if patch.PlanExpiresAt == nil {
			updates["plan_expires_at"] = gorm.Expr("NULL")
		} else {
			updates["plan_expires_at"] = patch.PlanExpiresAt.UTC()
		}
	}
	if patch.SetTrialEndsAt {
		if patch.TrialEndsAt == nil {
			updates["trial_ends_at"] = gorm.Expr("NULL")
		} else {
			updates["trial_ends_at"] = patch.TrialEndsAt.UTC()
		}
	}
	if patch.SetCanceledAt {
		if patch.CanceledAt == nil {
			updates["canceled_at"] = gorm.Expr("NULL")
		} else {
			updates["canceled_at"] = patch.CanceledAt.UTC()
		}
	}
	if patch.SetStatus {
		updates["subscription_status"] = domain.NormalizeSubscriptionStatus(patch.SubscriptionStatus)
	}
	if len(updates) == 0 {
		return r.GetByIDForUpdateTx(tx, userID)
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
// Prefer ListPastGracePaidUsers for the daily cron (respects grace period).
func (r *GormUserRepository) ListExpiredPaidUsers(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]domain.User, error) {
	return r.listPaidUsersExpiresAtOrBefore(ctx, now, limit)
}

// CountActivePremiumUsers counts users currently on a paid plan_tier.
func (r *GormUserRepository) CountActivePremiumUsers(ctx context.Context) (int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, err
	}
	var n int64
	err = db.WithContext(ctx).Model(&domain.User{}).
		Where("plan_tier IN ?", []domain.PlanTier{domain.PlanPremium, domain.PlanPremiumPlus}).
		Count(&n).Error
	return n, err
}

// UpcomingExpiry is a compact row for admin payment metrics.
type UpcomingExpiry struct {
	UserID        uuid.UUID
	Email         string
	PlanTier      domain.PlanTier
	PlanExpiresAt time.Time
}

// ListUpcomingExpiries returns paid users whose plan_expires_at is in [now, now+within].
func (r *GormUserRepository) ListUpcomingExpiries(
	ctx context.Context,
	now time.Time,
	within time.Duration,
	limit int,
) ([]UpcomingExpiry, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	from := now.UTC()
	to := from.Add(within)
	var rows []domain.User
	err = db.WithContext(ctx).
		Select("id", "email", "plan_tier", "plan_expires_at").
		Where("plan_tier IN ? AND plan_expires_at IS NOT NULL AND plan_expires_at >= ? AND plan_expires_at <= ?",
			[]domain.PlanTier{domain.PlanPremium, domain.PlanPremiumPlus},
			from, to,
		).
		Order("plan_expires_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]UpcomingExpiry, 0, len(rows))
	for i := range rows {
		if rows[i].PlanExpiresAt == nil {
			continue
		}
		out = append(out, UpcomingExpiry{
			UserID:        rows[i].ID,
			Email:         rows[i].Email,
			PlanTier:      domain.NormalizePlanTier(rows[i].PlanTier),
			PlanExpiresAt: rows[i].PlanExpiresAt.UTC(),
		})
	}
	return out, nil
}

// ListPastGracePaidUsers returns paid users whose grace window has ended
// (plan_expires_at + graceDays <= now). Used by the daily downgrade cron.
func (r *GormUserRepository) ListPastGracePaidUsers(
	ctx context.Context,
	now time.Time,
	graceDays int,
	limit int,
) ([]domain.User, error) {
	graceDays = domain.ClampGraceDays(graceDays)
	// plan_expires_at <= now - graceDays  ⇔  plan_expires_at + grace <= now
	cutoff := now.UTC().Add(-domain.DaysDuration(graceDays))
	return r.listPaidUsersExpiresAtOrBefore(ctx, cutoff, limit)
}

func (r *GormUserRepository) listPaidUsersExpiresAtOrBefore(
	ctx context.Context,
	expiresAtOrBefore time.Time,
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
			expiresAtOrBefore.UTC(),
		).
		Order("plan_expires_at ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}
