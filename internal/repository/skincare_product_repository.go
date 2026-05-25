package repository

import (
	"context"
	"fmt"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormSkincareProductRepository persists wardrobe items.
type GormSkincareProductRepository struct {
	db *gorm.DB
}

// NewSkincareProductRepository returns a product repository.
func NewSkincareProductRepository(db *gorm.DB) *GormSkincareProductRepository {
	return &GormSkincareProductRepository{db: db}
}

func (r *GormSkincareProductRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts a row.
func (r *GormSkincareProductRepository) Create(ctx context.Context, p *domain.SkincareProduct) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(p).Error
}

// ListByUser returns all active products for a user, newest first.
func (r *GormSkincareProductRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.SkincareProduct, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var rows []domain.SkincareProduct
	tx := db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&rows)
	if tx.Error != nil {
		return nil, tx.Error
	}
	return rows, nil
}
