package repository

import (
	"context"
	"errors"
	"fmt"

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
