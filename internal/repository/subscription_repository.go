package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SubscriptionRepository persists append-only subscription history rows.
type SubscriptionRepository struct {
	db *gorm.DB
}

// NewSubscriptionRepository returns a subscriptions history repository.
func NewSubscriptionRepository(db *gorm.DB) *SubscriptionRepository {
	return &SubscriptionRepository{db: db}
}

func (r *SubscriptionRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts one history row.
func (r *SubscriptionRepository) Create(ctx context.Context, row *domain.Subscription) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("subscription row required")
	}
	return db.WithContext(ctx).Create(row).Error
}

// CreateTx inserts within an existing transaction.
func (r *SubscriptionRepository) CreateTx(tx *gorm.DB, row *domain.Subscription) error {
	if tx == nil {
		return fmt.Errorf("transaction required")
	}
	if row == nil {
		return fmt.Errorf("subscription row required")
	}
	return tx.Create(row).Error
}

// ListForUser returns newest-first history for a user.
func (r *SubscriptionRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
	limit int,
) ([]domain.Subscription, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var rows []domain.Subscription
	err = db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// ExistsByExternalRef reports whether a history row already used this provider ref
// (idempotency helper for SePay renewals).
func (r *SubscriptionRepository) ExistsByExternalRef(ctx context.Context, externalRef string) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	externalRef = strings.TrimSpace(externalRef)
	if externalRef == "" {
		return false, nil
	}
	var count int64
	err = db.WithContext(ctx).
		Model(&domain.Subscription{}).
		Where("external_ref = ?", externalRef).
		Count(&count).Error
	return count > 0, err
}
