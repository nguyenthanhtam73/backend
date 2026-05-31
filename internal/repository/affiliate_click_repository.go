package repository

import (
	"context"
	"fmt"

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
