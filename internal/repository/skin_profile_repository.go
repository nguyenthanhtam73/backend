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
)

// GormSkinProfileRepository persists SkinProfile rows (one per user).
type GormSkinProfileRepository struct {
	db *gorm.DB
}

// NewSkinProfileRepository returns a profile repository using GORM.
func NewSkinProfileRepository(db *gorm.DB) *GormSkinProfileRepository {
	return &GormSkinProfileRepository{db: db}
}

func (r *GormSkinProfileRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// GetByUserID returns the skin profile for a user or nil.
func (r *GormSkinProfileRepository) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.SkinProfile, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var p domain.SkinProfile
	tx := db.WithContext(ctx).Where("user_id = ?", userID).First(&p)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &p, nil
}

// DeleteByUserID soft-deletes the skin profile row for a user.
func (r *GormSkinProfileRepository) DeleteByUserID(ctx context.Context, userID uuid.UUID) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if userID == uuid.Nil {
		return fmt.Errorf("invalid user id")
	}
	return db.WithContext(ctx).Where("user_id = ?", userID).Delete(&domain.SkinProfile{}).Error
}

// getByUserIDIncludingDeleted returns the profile row even when soft-deleted.
// Needed for UpsertForUser: unique index on user_id still blocks a second INSERT
// while GORM's default queries hide deleted rows.
func (r *GormSkinProfileRepository) getByUserIDIncludingDeleted(ctx context.Context, userID uuid.UUID) (*domain.SkinProfile, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var p domain.SkinProfile
	tx := db.WithContext(ctx).Unscoped().Where("user_id = ?", userID).First(&p)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &p, nil
}

// ListTouchedSince returns profiles created or updated at or after since,
// oldest created_at first. Soft-deleted rows are omitted.
func (r *GormSkinProfileRepository) ListTouchedSince(ctx context.Context, since time.Time) ([]domain.SkinProfile, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var rows []domain.SkinProfile
	err = db.WithContext(ctx).
		Where("created_at >= ? OR updated_at >= ?", since.UTC(), since.UTC()).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

// UpsertForUser creates or updates the single SkinProfile for this user.
func (r *GormSkinProfileRepository) UpsertForUser(ctx context.Context, p *domain.SkinProfile) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if p == nil || p.UserID == uuid.Nil {
		return fmt.Errorf("invalid profile")
	}
	existing, err := r.getByUserIDIncludingDeleted(ctx, p.UserID)
	if err != nil {
		return err
	}
	if existing == nil {
		return db.WithContext(ctx).Create(p).Error
	}
	p.ID = existing.ID
	if existing.DeletedAt.Valid {
		p.DeletedAt = gorm.DeletedAt{}
		return db.WithContext(ctx).Unscoped().Save(p).Error
	}
	return db.WithContext(ctx).Save(p).Error
}

// StarterClaimStaleAge is how long a starter-generation lease is honored.
// A crashed process stops blocking a retry after this.
const StarterClaimStaleAge = 5 * time.Minute

// ClaimStarterGeneration leases starter generation for one user. The UPDATE
// matches only an empty or stale lease, so a second process (API or CLI) gets
// false and must not call the model. token is stored so only the owner can release.
func (r *GormSkinProfileRepository) ClaimStarterGeneration(ctx context.Context, userID uuid.UUID, now time.Time) (token string, won bool, err error) {
	db, err := r.dbOrErr()
	if err != nil {
		return "", false, err
	}
	if userID == uuid.Nil {
		return "", false, fmt.Errorf("invalid profile")
	}
	now = now.UTC()
	token = uuid.NewString()
	staleBefore := now.Add(-StarterClaimStaleAge)
	res := db.WithContext(ctx).Exec(`
UPDATE skin_profiles
SET starter_claim_token = ?, starter_claim_at = ?, updated_at = ?
WHERE user_id = ? AND deleted_at IS NULL AND (
    starter_claim_token = '' OR starter_claim_token IS NULL
    OR starter_claim_at IS NULL OR starter_claim_at < ?
)`,
		token, now, now, userID, staleBefore,
	)
	if res.Error != nil {
		return "", false, res.Error
	}
	if res.RowsAffected != 1 {
		return "", false, nil
	}
	return token, true, nil
}

// ReleaseStarterGeneration drops a lease this caller owns. A different token is left alone.
func (r *GormSkinProfileRepository) ReleaseStarterGeneration(ctx context.Context, userID uuid.UUID, token string) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if userID == uuid.Nil || token == "" {
		return nil
	}
	return db.WithContext(ctx).Exec(`
UPDATE skin_profiles
SET starter_claim_token = '', starter_claim_at = NULL
WHERE user_id = ? AND starter_claim_token = ?`,
		userID, token,
	).Error
}
