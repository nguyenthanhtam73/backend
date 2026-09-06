package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormPushJobLockRepository persists daily push job claims across processes.
type GormPushJobLockRepository struct {
	db *gorm.DB
}

// NewPushJobLockRepository constructs the repository.
func NewPushJobLockRepository(db *gorm.DB) *GormPushJobLockRepository {
	return &GormPushJobLockRepository{db: db}
}

func (r *GormPushJobLockRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// TryClaim attempts to claim jobName for runDate (VN "2006-01-02").
// Returns true when this caller owns the claim; false if another process
// already holds a non-expired claim for the same day.
//
// An expired lease (pod crash) may be stolen so the evening job can finish.
func (r *GormPushJobLockRepository) TryClaim(
	ctx context.Context,
	jobName, runDate string,
) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	if jobName == "" || runDate == "" {
		return false, fmt.Errorf("job name and run date required")
	}

	conn := DBFromContext(ctx, db)
	var claimed bool
	err = conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row domain.PushJobLock
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("job_name = ?", jobName).
			First(&row)
		now := time.Now().UTC()
		expires := now.Add(domain.PushJobLeaseDuration)

		if errors.Is(q.Error, gorm.ErrRecordNotFound) {
			claimed = true
			return tx.Create(&domain.PushJobLock{
				JobName:     jobName,
				LastRunDate: runDate,
				ClaimedAt:   now,
				ExpiresAt:   expires,
				UpdatedAt:   now,
			}).Error
		}
		if q.Error != nil {
			return q.Error
		}

		sameDay := row.LastRunDate == runDate
		expired := sameDay && !row.ExpiresAt.IsZero() && now.After(row.ExpiresAt)

		if sameDay && !expired {
			claimed = false
			return nil
		}
		// New day, unlocked (empty last_run_date), or expired lease — take claim.
		claimed = true
		return tx.Model(&row).Updates(map[string]any{
			"last_run_date": runDate,
			"claimed_at":    now,
			"expires_at":    expires,
			"updated_at":    now,
		}).Error
	})
	return claimed, err
}

// ReleaseClaim clears today's claim so the job can retry.
// No-op if the row is missing or already holds a different day.
func (r *GormPushJobLockRepository) ReleaseClaim(
	ctx context.Context,
	jobName, runDate string,
) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	conn := DBFromContext(ctx, db)
	return conn.WithContext(ctx).
		Model(&domain.PushJobLock{}).
		Where("job_name = ? AND last_run_date = ?", jobName, runDate).
		Updates(map[string]any{
			"last_run_date": "",
			"expires_at":    time.Time{},
			"updated_at":    time.Now().UTC(),
		}).Error
}
