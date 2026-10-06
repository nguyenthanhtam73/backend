package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EmailEngagementRepository stores Resend open/click events.
type EmailEngagementRepository struct {
	db *gorm.DB
}

// NewEmailEngagementRepository constructs the repository.
func NewEmailEngagementRepository(db *gorm.DB) *EmailEngagementRepository {
	return &EmailEngagementRepository{db: db}
}

func (r *EmailEngagementRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// InsertIfNew inserts one event. inserted is false when resend_event_id
// was already stored (duplicate webhook delivery).
func (r *EmailEngagementRepository) InsertIfNew(
	ctx context.Context,
	ev *domain.EmailEngagementEvent,
) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	if ev == nil || strings.TrimSpace(ev.ResendEventID) == "" {
		return false, fmt.Errorf("resend event id required")
	}
	ev.ResendEventID = strings.TrimSpace(ev.ResendEventID)
	tx := db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "resend_event_id"}},
			DoNothing: true,
		}).
		Create(ev)
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}

// CountByResendEventID is for tests and ops checks.
func (r *EmailEngagementRepository) CountByResendEventID(
	ctx context.Context,
	resendEventID string,
) (int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, err
	}
	var n int64
	err = db.WithContext(ctx).
		Model(&domain.EmailEngagementEvent{}).
		Where("resend_event_id = ?", resendEventID).
		Count(&n).Error
	return n, err
}
