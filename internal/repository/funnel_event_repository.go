package repository

import (
	"context"
	"fmt"

	"github.com/dadiary/backend/internal/domain"
	"gorm.io/gorm"
)

// GormFunnelEventRepository persists check-in funnel events.
type GormFunnelEventRepository struct {
	db *gorm.DB
}

// NewFunnelEventRepository returns a funnel-event repository.
func NewFunnelEventRepository(db *gorm.DB) *GormFunnelEventRepository {
	return &GormFunnelEventRepository{db: db}
}

func (r *GormFunnelEventRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts one funnel event row.
func (r *GormFunnelEventRepository) Create(ctx context.Context, row *domain.FunnelEvent) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(row).Error
}
