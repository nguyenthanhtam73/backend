package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"gorm.io/gorm"
)

// GormAffiliateClickRepository persists affiliate link click events.
type GormAffiliateClickRepository struct {
	db *gorm.DB
}

// NewAffiliateClickRepository returns an affiliate click repository.
func NewAffiliateClickRepository(db *gorm.DB) *GormAffiliateClickRepository {
	return &GormAffiliateClickRepository{db: db}
}

func (r *GormAffiliateClickRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts one click row.
func (r *GormAffiliateClickRepository) Create(ctx context.Context, row *domain.AffiliateClick) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(row).Error
}

// AffiliateClickSKUAgg is one grouped click row for admin metrics.
type AffiliateClickSKUAgg struct {
	ProductName   string
	Brand         string
	AffiliateLink string
	Clicks        int64
	LastClickAt   time.Time
}

// CountSince returns total clicks with created_at >= since (UTC).
func (r *GormAffiliateClickRepository) CountSince(ctx context.Context, since time.Time) (int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, err
	}
	var n int64
	err = db.WithContext(ctx).Model(&domain.AffiliateClick{}).
		Where("created_at >= ?", since).
		Count(&n).Error
	return n, err
}

// CountAll returns total click rows.
func (r *GormAffiliateClickRepository) CountAll(ctx context.Context) (int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, err
	}
	var n int64
	err = db.WithContext(ctx).Model(&domain.AffiliateClick{}).Count(&n).Error
	return n, err
}

// AggregateBySKUSince groups clicks by product_name+brand+affiliate_link since a time.
func (r *GormAffiliateClickRepository) AggregateBySKUSince(ctx context.Context, since time.Time, limit int) ([]AffiliateClickSKUAgg, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []AffiliateClickSKUAgg
	err = db.WithContext(ctx).Model(&domain.AffiliateClick{}).
		Select("product_name, brand, affiliate_link, COUNT(*) AS clicks, MAX(created_at) AS last_click_at").
		Where("created_at >= ?", since).
		Group("product_name, brand, affiliate_link").
		Order("clicks DESC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}
