package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"gorm.io/gorm"
)

// GormPaywallViewRepository persists paywall impression rows.
type GormPaywallViewRepository struct {
	db *gorm.DB
}

// NewPaywallViewRepository returns a paywall-view repository.
func NewPaywallViewRepository(db *gorm.DB) *GormPaywallViewRepository {
	return &GormPaywallViewRepository{db: db}
}

func (r *GormPaywallViewRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts one impression row.
func (r *GormPaywallViewRepository) Create(ctx context.Context, row *domain.PaywallView) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(row).Error
}

// CountSince returns impression rows with created_at >= since.
func (r *GormPaywallViewRepository) CountSince(ctx context.Context, since time.Time) (int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, err
	}
	var n int64
	err = db.WithContext(ctx).Model(&domain.PaywallView{}).
		Where("created_at >= ?", since).
		Count(&n).Error
	return n, err
}
