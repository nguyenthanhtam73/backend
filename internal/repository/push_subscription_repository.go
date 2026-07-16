package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormPushSubscriptionRepository persists Web Push subscriptions.
type GormPushSubscriptionRepository struct {
	db *gorm.DB
}

// NewPushSubscriptionRepository constructs the repository.
func NewPushSubscriptionRepository(db *gorm.DB) *GormPushSubscriptionRepository {
	return &GormPushSubscriptionRepository{db: db}
}

func (r *GormPushSubscriptionRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts a push subscription row.
func (r *GormPushSubscriptionRepository) Create(
	ctx context.Context,
	subscription *domain.PushSubscription,
) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if subscription == nil {
		return fmt.Errorf("subscription required")
	}
	return db.WithContext(ctx).Create(subscription).Error
}

// GetByUserID returns the newest active subscription for a user, or (nil, nil).
// Phase 1 keeps at most one row per user; newest-first is defensive.
func (r *GormPushSubscriptionRepository) GetByUserID(
	ctx context.Context,
	userID uuid.UUID,
) (*domain.PushSubscription, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	var row domain.PushSubscription
	tx := db.WithContext(ctx).
		Where("user_id = ? AND is_active = ?", userID, true).
		Order("updated_at DESC").
		First(&row)
	if tx.Error != nil {
		if tx.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// DeleteByUserID removes all push subscriptions for a user.
func (r *GormPushSubscriptionRepository) DeleteByUserID(
	ctx context.Context,
	userID uuid.UUID,
) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user id required")
	}
	return db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&domain.PushSubscription{}).Error
}

// DeleteByEndpoint removes a subscription by push endpoint URL
// (e.g. when the same browser re-subscribes under a different account).
func (r *GormPushSubscriptionRepository) DeleteByEndpoint(
	ctx context.Context,
	endpoint string,
) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return fmt.Errorf("endpoint required")
	}
	return db.WithContext(ctx).
		Where("endpoint = ?", endpoint).
		Delete(&domain.PushSubscription{}).Error
}

// ListActiveUserIDs returns distinct user IDs that currently have an active
// push subscription. Used by daily-reminder fan-out (and future jobs).
func (r *GormPushSubscriptionRepository) ListActiveUserIDs(
	ctx context.Context,
) ([]uuid.UUID, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	err = db.WithContext(ctx).
		Model(&domain.PushSubscription{}).
		Where("is_active = ?", true).
		Distinct("user_id").
		Pluck("user_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}
