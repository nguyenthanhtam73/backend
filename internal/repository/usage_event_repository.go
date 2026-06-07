package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UsageEventRepository persists monthly usage counters.
type UsageEventRepository interface {
	Create(ctx context.Context, event *domain.UsageEvent) error
	CountSince(ctx context.Context, userID uuid.UUID, feature domain.UsageFeature, since time.Time) (int64, error)
}

// GormUsageEventRepository is the Postgres-backed UsageEventRepository.
type GormUsageEventRepository struct {
	db *gorm.DB
}

// NewUsageEventRepository returns a UsageEventRepository backed by GORM.
func NewUsageEventRepository(db *gorm.DB) *GormUsageEventRepository {
	return &GormUsageEventRepository{db: db}
}

func (r *GormUsageEventRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

func (r *GormUsageEventRepository) Create(ctx context.Context, event *domain.UsageEvent) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(event).Error
}

func (r *GormUsageEventRepository) CountSince(
	ctx context.Context,
	userID uuid.UUID,
	feature domain.UsageFeature,
	since time.Time,
) (int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, err
	}
	var n int64
	tx := db.WithContext(ctx).Model(&domain.UsageEvent{}).
		Where("user_id = ? AND feature = ? AND created_at >= ?", userID, feature, since).
		Count(&n)
	return n, tx.Error
}
