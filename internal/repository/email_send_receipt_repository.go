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

// EmailSendReceiptRepository persists ≤1 D0 and ≤1 D1 reminder email per user.
type EmailSendReceiptRepository struct {
	db *gorm.DB
}

// NewEmailSendReceiptRepository constructs the repository.
func NewEmailSendReceiptRepository(db *gorm.DB) *EmailSendReceiptRepository {
	return &EmailSendReceiptRepository{db: db}
}

func (r *EmailSendReceiptRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// HasSent reports whether this user already received a kind (d0|d1) email.
func (r *EmailSendReceiptRepository) HasSent(
	ctx context.Context,
	userID uuid.UUID,
	kind string,
) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	var n int64
	err = db.WithContext(ctx).
		Model(&domain.EmailSendReceipt{}).
		Where("user_id = ? AND kind = ?", userID, kind).
		Count(&n).Error
	return n > 0, err
}

// TryClaim inserts a receipt row. Returns true when this caller owns the send
// (first insert). False means another replica already claimed / sent.
func (r *EmailSendReceiptRepository) TryClaim(
	ctx context.Context,
	userID uuid.UUID,
	kind string,
) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	if userID == uuid.Nil || kind == "" {
		return false, fmt.Errorf("user id and kind required")
	}
	tx := db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&domain.EmailSendReceipt{
			UserID:    userID,
			Kind:      kind,
			CreatedAt: time.Now().UTC(),
		})
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}

// Release deletes a claim so a failed send can retry.
func (r *EmailSendReceiptRepository) Release(
	ctx context.Context,
	userID uuid.UUID,
	kind string,
) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).
		Where("user_id = ? AND kind = ?", userID, kind).
		Delete(&domain.EmailSendReceipt{}).Error
}
