package repository

import (
	"context"
	"errors"
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

// CountByUser returns the number of active (non-deleted) products for a user.
func (r *GormSkincareProductRepository) CountByUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, err
	}
	var n int64
	tx := db.WithContext(ctx).Model(&domain.SkincareProduct{}).Where("user_id = ?", userID).Count(&n)
	return n, tx.Error
}

// GetByIDForUser loads one active product owned by userID.
func (r *GormSkincareProductRepository) GetByIDForUser(
	ctx context.Context,
	userID, productID uuid.UUID,
) (*domain.SkincareProduct, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var row domain.SkincareProduct
	tx := db.WithContext(ctx).Where("id = ? AND user_id = ?", productID, userID).First(&row)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// Update persists field changes on an existing row.
func (r *GormSkincareProductRepository) Update(ctx context.Context, p *domain.SkincareProduct) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Save(p).Error
}

// SoftDelete marks a product deleted (GORM DeletedAt) when owned by userID.
// Returns false when no matching row exists.
func (r *GormSkincareProductRepository) SoftDelete(
	ctx context.Context,
	userID, productID uuid.UUID,
) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	tx := db.WithContext(ctx).
		Where("id = ? AND user_id = ?", productID, userID).
		Delete(&domain.SkincareProduct{})
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}
