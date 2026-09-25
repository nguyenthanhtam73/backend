package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// CreateUnderFreeCap inserts a product only if the user is still under `cap`
// live rows. The user row is locked on Postgres so concurrent Free creates
// cannot both slip under the cap.
func (r *GormSkincareProductRepository) CreateUnderFreeCap(
	ctx context.Context,
	p *domain.SkincareProduct,
	cap int,
) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if p == nil || p.UserID == uuid.Nil {
		return fmt.Errorf("invalid product")
	}
	if cap <= 0 {
		return ErrShelfCapExceeded
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&domain.User{}).Select("id").Where("id = ?", p.UserID)
		if tx.Dialector.Name() == "postgres" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var owner domain.User
		if err := q.First(&owner).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&domain.SkincareProduct{}).
			Where("user_id = ?", p.UserID).
			Count(&n).Error; err != nil {
			return err
		}
		if int(n) >= cap {
			return ErrShelfCapExceeded
		}
		return tx.Create(p).Error
	})
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

// SetInsight writes the cabinet card without touching name, brand, or notes.
func (r *GormSkincareProductRepository) SetInsight(
	ctx context.Context,
	userID, productID uuid.UUID,
	raw json.RawMessage,
	at time.Time,
) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	if userID == uuid.Nil || productID == uuid.Nil || len(raw) == 0 {
		return false, fmt.Errorf("invalid insight")
	}
	tx := db.WithContext(ctx).Model(&domain.SkincareProduct{}).
		Where("id = ? AND user_id = ?", productID, userID).
		Updates(map[string]any{
			"insight":    raw,
			"insight_at": at.UTC(),
		})
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}

// ReplaceInsightCopy writes a cabinet card onto one owned row.
// Only insight and insight_at change. updated_at, name, brand, notes, and
// every other column stay as they are. The backfill command uses this so a
// wording fix does not look like the user edited the product.
func (r *GormSkincareProductRepository) ReplaceInsightCopy(
	ctx context.Context,
	userID, productID uuid.UUID,
	raw json.RawMessage,
	at time.Time,
) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	if userID == uuid.Nil || productID == uuid.Nil || len(raw) == 0 {
		return false, fmt.Errorf("invalid insight")
	}
	tx := db.WithContext(ctx).Model(&domain.SkincareProduct{}).
		Where("id = ? AND user_id = ?", productID, userID).
		UpdateColumns(map[string]any{
			"insight":    raw,
			"insight_at": at.UTC(),
		})
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}

// ClearInsight drops a stale card after the product name, brand, category, or notes change.
func (r *GormSkincareProductRepository) ClearInsight(ctx context.Context, userID, productID uuid.UUID) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	tx := db.WithContext(ctx).Model(&domain.SkincareProduct{}).
		Where("id = ? AND user_id = ?", productID, userID).
		Updates(map[string]any{
			"insight":    gorm.Expr("NULL"),
			"insight_at": gorm.Expr("NULL"),
		})
	return tx.Error
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
