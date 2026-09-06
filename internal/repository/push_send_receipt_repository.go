package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormPushSendReceiptRepository persists successful push deliveries per user/day.
type GormPushSendReceiptRepository struct {
	db *gorm.DB
}

// NewPushSendReceiptRepository constructs the repository.
func NewPushSendReceiptRepository(db *gorm.DB) *GormPushSendReceiptRepository {
	return &GormPushSendReceiptRepository{db: db}
}

func (r *GormPushSendReceiptRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// HasSent reports whether userID already received notificationType on runDate.
func (r *GormPushSendReceiptRepository) HasSent(
	ctx context.Context,
	userID uuid.UUID,
	notificationType, runDate string,
) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	var n int64
	err = DBFromContext(ctx, db).WithContext(ctx).
		Model(&domain.PushSendReceipt{}).
		Where(
			"user_id = ? AND notification_type = ? AND run_date = ?",
			userID, notificationType, runDate,
		).
		Count(&n).Error
	return n > 0, err
}

// HasSentAny reports whether userID already received any of notificationTypes on runDate.
func (r *GormPushSendReceiptRepository) HasSentAny(
	ctx context.Context,
	userID uuid.UUID,
	runDate string,
	notificationTypes ...string,
) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	if len(notificationTypes) == 0 {
		return false, nil
	}
	var n int64
	err = DBFromContext(ctx, db).WithContext(ctx).
		Model(&domain.PushSendReceipt{}).
		Where("user_id = ? AND run_date = ? AND notification_type IN ?",
			userID, runDate, notificationTypes).
		Count(&n).Error
	return n > 0, err
}

// MarkSent records a successful delivery (idempotent on conflict).
func (r *GormPushSendReceiptRepository) MarkSent(
	ctx context.Context,
	userID uuid.UUID,
	notificationType, runDate string,
) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	row := domain.PushSendReceipt{
		UserID:           userID,
		NotificationType: notificationType,
		RunDate:          runDate,
		CreatedAt:        time.Now().UTC(),
	}
	return DBFromContext(ctx, db).WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&row).Error
}
