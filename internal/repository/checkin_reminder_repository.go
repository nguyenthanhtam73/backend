package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CheckInReminderRepository persists D0/D1 reminder snapshots.
type CheckInReminderRepository struct {
	db *gorm.DB
}

// NewCheckInReminderRepository returns a reminder-flag repository.
func NewCheckInReminderRepository(db *gorm.DB) *CheckInReminderRepository {
	return &CheckInReminderRepository{db: db}
}

func (r *CheckInReminderRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// GetByUserID loads the latest snapshot, or nil when none exists.
func (r *CheckInReminderRepository) GetByUserID(
	ctx context.Context,
	userID uuid.UUID,
) (*domain.CheckInReminderFlag, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	var row domain.CheckInReminderFlag
	tx := db.WithContext(ctx).Where("user_id = ?", userID).First(&row)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// Upsert writes the computed snapshot (idempotent on user_id).
func (r *CheckInReminderRepository) Upsert(ctx context.Context, row *domain.CheckInReminderFlag) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if row == nil || row.UserID == uuid.Nil {
		return fmt.Errorf("reminder flag required")
	}
	now := time.Now().UTC()
	if row.ComputedAt.IsZero() {
		row.ComputedAt = now
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"kind",
			"due",
			"signup_date",
			"checked_in_today",
			"days_since_signup",
			"computed_on",
			"computed_at",
			"updated_at",
			"deleted_at",
		}),
	}).Create(row).Error
}

// ListDueUserIDs returns users currently marked due (for refresh / CLI).
func (r *CheckInReminderRepository) ListDueUserIDs(ctx context.Context, limit int) ([]uuid.UUID, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 5000 {
		limit = 2000
	}
	var ids []uuid.UUID
	err = db.WithContext(ctx).
		Model(&domain.CheckInReminderFlag{}).
		Where("due = ?", true).
		Limit(limit).
		Pluck("user_id", &ids).Error
	return ids, err
}
