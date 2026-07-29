package repository

import (
	"context"
	"fmt"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormAdminSkinReviewRepository persists admin skin observation sessions.
type GormAdminSkinReviewRepository struct {
	db *gorm.DB
}

// NewAdminSkinReviewRepository constructs the repository.
func NewAdminSkinReviewRepository(db *gorm.DB) *GormAdminSkinReviewRepository {
	return &GormAdminSkinReviewRepository{db: db}
}

func (r *GormAdminSkinReviewRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts a new admin skin review row.
func (r *GormAdminSkinReviewRepository) Create(ctx context.Context, row *domain.AdminSkinReview) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(row).Error
}

// GetByID loads one review by primary key (any admin may read — console is shared).
func (r *GormAdminSkinReviewRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.AdminSkinReview, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("admin skin review id required")
	}
	var row domain.AdminSkinReview
	tx := db.WithContext(ctx).Where("id = ?", id).First(&row)
	if tx.Error != nil {
		if tx.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// UpdateMeta patches title / notes / status for one review.
func (r *GormAdminSkinReviewRepository) UpdateMeta(
	ctx context.Context,
	id uuid.UUID,
	title *string,
	notes *string,
	status *string,
) (*domain.AdminSkinReview, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("admin skin review id required")
	}

	updates := map[string]any{}
	if title != nil {
		updates["title"] = *title
	}
	if notes != nil {
		updates["notes"] = *notes
	}
	if status != nil {
		updates["status"] = *status
	}
	if len(updates) == 0 {
		return r.GetByID(ctx, id)
	}

	res := db.WithContext(ctx).
		Model(&domain.AdminSkinReview{}).
		Where("id = ?", id).
		Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.GetByID(ctx, id)
}
