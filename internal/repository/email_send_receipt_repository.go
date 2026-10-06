package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EmailSendReceiptRepository persists ≤1 reminder email per kind per user (d0, d1, d3).
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

// HasSent reports whether this user already received a kind (d0|d1|d3) email.
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

// SetResendEmailID stores Resend's email id on a claimed receipt so opens and
// clicks can join. Empty id is a no-op. A later id does not overwrite.
func (r *EmailSendReceiptRepository) SetResendEmailID(
	ctx context.Context,
	userID uuid.UUID,
	kind string,
	resendEmailID string,
) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	resendEmailID = strings.TrimSpace(resendEmailID)
	if userID == uuid.Nil || kind == "" || resendEmailID == "" {
		return nil
	}
	return db.WithContext(ctx).
		Model(&domain.EmailSendReceipt{}).
		Where("user_id = ? AND kind = ? AND (resend_email_id = '' OR resend_email_id IS NULL)", userID, kind).
		Update("resend_email_id", resendEmailID).Error
}

// FindByResendEmailID returns the receipt for a Resend email id, or (nil, nil).
func (r *EmailSendReceiptRepository) FindByResendEmailID(
	ctx context.Context,
	resendEmailID string,
) (*domain.EmailSendReceipt, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	resendEmailID = strings.TrimSpace(resendEmailID)
	if resendEmailID == "" {
		return nil, nil
	}
	var row domain.EmailSendReceipt
	err = db.WithContext(ctx).
		Where("resend_email_id = ?", resendEmailID).
		Limit(1).
		Find(&row).Error
	if err != nil {
		return nil, err
	}
	if row.UserID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}
