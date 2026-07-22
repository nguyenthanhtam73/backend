package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PaymentOpsEventRepository persists payment monitoring events.
type PaymentOpsEventRepository struct {
	db *gorm.DB
}

// NewPaymentOpsEventRepository constructs the repo.
func NewPaymentOpsEventRepository(db *gorm.DB) *PaymentOpsEventRepository {
	return &PaymentOpsEventRepository{db: db}
}

func (r *PaymentOpsEventRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts one ops event (best-effort callers may ignore errors).
func (r *PaymentOpsEventRepository) Create(ctx context.Context, ev *domain.PaymentOpsEvent) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if ev.ID == uuid.Nil {
		ev.ID = uuid.New()
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now().UTC()
	}
	return db.WithContext(ctx).Create(ev).Error
}

// CountSince returns how many events of kind were created at/after since.
func (r *PaymentOpsEventRepository) CountSince(ctx context.Context, kind string, since time.Time) (int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, err
	}
	var n int64
	err = db.WithContext(ctx).Model(&domain.PaymentOpsEvent{}).
		Where("kind = ? AND created_at >= ?", kind, since.UTC()).
		Count(&n).Error
	return n, err
}
