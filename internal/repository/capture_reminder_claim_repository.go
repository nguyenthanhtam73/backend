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

// CaptureReminderClaimRepository stores the one-moment-per-local-day lock.
type CaptureReminderClaimRepository struct {
	db *gorm.DB
}

// NewCaptureReminderClaimRepository constructs the repository.
func NewCaptureReminderClaimRepository(db *gorm.DB) *CaptureReminderClaimRepository {
	return &CaptureReminderClaimRepository{db: db}
}

func (r *CaptureReminderClaimRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// TryClaim inserts the (user, local date) row. True means this caller owns
// the moment. False means another tick or replica already claimed it.
func (r *CaptureReminderClaimRepository) TryClaim(
	ctx context.Context,
	userID uuid.UUID,
	localDate string,
	at time.Time,
) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	if userID == uuid.Nil || len(localDate) != 10 {
		return false, fmt.Errorf("user id and local date required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	tx := db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "local_date"}},
			DoNothing: true,
		}).
		Create(&domain.CaptureReminderClaim{
			UserID:    userID,
			LocalDate: localDate,
			ClaimedAt: at.UTC(),
		})
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}

// Release deletes a claim so a failed send can retry inside the grace window.
func (r *CaptureReminderClaimRepository) Release(
	ctx context.Context,
	userID uuid.UUID,
	localDate string,
) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).
		Where("user_id = ? AND local_date = ?", userID, localDate).
		Delete(&domain.CaptureReminderClaim{}).Error
}
