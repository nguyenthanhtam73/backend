package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UsageView is the current-period snapshot returned by GetUsage.
type UsageView struct {
	UserID      uuid.UUID
	FeatureKey  string
	UsageCount  int
	PeriodKey   string
	PeriodStart time.Time
	PeriodEnd   time.Time
	// Limit is the plan cap passed in (−1 = unlimited).
	Limit int
	// Remaining is max(0, Limit−UsageCount). −1 when unlimited.
	Remaining int
	// Unlimited is true when Limit < 0.
	Unlimited bool
}

// UserUsageRepository persists monthly feature counters.
type UserUsageRepository interface {
	// IncrementUsage bumps the counter for the current UTC month.
	// When maxCount > 0, the bump is applied only if usage_count < maxCount
	// (safe under concurrent requests). Returns the resulting count and whether
	// the increment landed.
	IncrementUsage(ctx context.Context, userID uuid.UUID, featureKey string, maxCount int) (newCount int, incremented bool, err error)

	// GetUsage returns used/remaining for the current UTC month.
	// monthlyLimit: plan cap (−1 = unlimited → Remaining = −1).
	GetUsage(ctx context.Context, userID uuid.UUID, featureKey string, monthlyLimit int) (UsageView, error)

	// ResetMonthlyUsage deletes rows whose period_end is at or before cutoff
	// (normally current month period_start). Live metering keys off period_key,
	// so this is idempotent cleanup for the monthly cron.
	ResetMonthlyUsage(ctx context.Context, cutoffPeriodEnd time.Time) (deleted int64, err error)
}

// GormUserUsageRepository is the Postgres-backed UserUsageRepository.
type GormUserUsageRepository struct {
	db *gorm.DB
}

// NewUserUsageRepository returns a UserUsageRepository backed by GORM.
func NewUserUsageRepository(db *gorm.DB) *GormUserUsageRepository {
	return &GormUserUsageRepository{db: db}
}

func (r *GormUserUsageRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

func (r *GormUserUsageRepository) IncrementUsage(
	ctx context.Context,
	userID uuid.UUID,
	featureKey string,
	maxCount int,
) (int, bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, false, err
	}
	if userID == uuid.Nil {
		return 0, false, fmt.Errorf("user id required")
	}
	if featureKey == "" {
		return 0, false, fmt.Errorf("feature key required")
	}

	periodStart, periodEnd, periodKey := domain.CurrentUTCMonthPeriod(time.Now())
	now := time.Now().UTC()

	var newCount int
	incremented := false

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row domain.UserUsage
		// Avoid selecting TIMESTAMP columns here — SQLite drivers often return
		// them as strings; we only need id + usage_count for the bump.
		q := tx.Select("id", "user_id", "feature_key", "usage_count", "period_key").
			Where(
				"user_id = ? AND feature_key = ? AND period_key = ?",
				userID, featureKey, periodKey,
			).First(&row)

		if errors.Is(q.Error, gorm.ErrRecordNotFound) {
			row = domain.UserUsage{
				ID:          uuid.New(),
				UserID:      userID,
				FeatureKey:  featureKey,
				UsageCount:  1,
				PeriodKey:   periodKey,
				PeriodStart: periodStart,
				PeriodEnd:   periodEnd,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if cErr := tx.Create(&row).Error; cErr != nil {
				if !errors.Is(cErr, gorm.ErrDuplicatedKey) && !isUniqueViolation(cErr) {
					return cErr
				}
				if rErr := tx.Select("id", "user_id", "feature_key", "usage_count", "period_key").
					Where(
						"user_id = ? AND feature_key = ? AND period_key = ?",
						userID, featureKey, periodKey,
					).First(&row).Error; rErr != nil {
					return rErr
				}
				return bumpRowAtomic(tx, &row, maxCount, now, &newCount, &incremented)
			}
			newCount = 1
			incremented = true
			return nil
		}
		if q.Error != nil {
			return q.Error
		}
		return bumpRowAtomic(tx, &row, maxCount, now, &newCount, &incremented)
	})
	if err != nil {
		return 0, false, err
	}

	if incremented {
		slog.Info("user_usage: incremented",
			"user_id", userID.String(),
			"feature_key", featureKey,
			"usage_count", newCount,
			"max_count", maxCount,
			"period_key", periodKey,
		)
	} else {
		slog.Info("user_usage: increment blocked (quota)",
			"user_id", userID.String(),
			"feature_key", featureKey,
			"usage_count", newCount,
			"max_count", maxCount,
			"period_key", periodKey,
		)
	}
	return newCount, incremented, nil
}

// bumpRowAtomic increments with a compare-and-set on usage_count so two
// concurrent transactions cannot both pass a stale in-memory check.
func bumpRowAtomic(
	tx *gorm.DB,
	row *domain.UserUsage,
	maxCount int,
	now time.Time,
	newCount *int,
	incremented *bool,
) error {
	if maxCount > 0 && row.UsageCount >= maxCount {
		*newCount = row.UsageCount
		*incremented = false
		return nil
	}
	next := row.UsageCount + 1
	q := tx.Model(&domain.UserUsage{}).
		Where("id = ? AND usage_count = ?", row.ID, row.UsageCount)
	if maxCount > 0 {
		q = q.Where("usage_count < ?", maxCount)
	}
	res := q.UpdateColumns(map[string]any{
		"usage_count": next,
		"updated_at":  now,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var fresh domain.UserUsage
		if err := tx.Select("id", "usage_count").Where("id = ?", row.ID).First(&fresh).Error; err != nil {
			return err
		}
		*newCount = fresh.UsageCount
		*incremented = false
		return nil
	}
	*newCount = next
	*incremented = true
	return nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "violates unique constraint")
}

func (r *GormUserUsageRepository) GetUsage(
	ctx context.Context,
	userID uuid.UUID,
	featureKey string,
	monthlyLimit int,
) (UsageView, error) {
	var out UsageView
	out.UserID = userID
	out.FeatureKey = featureKey
	out.Limit = monthlyLimit

	periodStart, periodEnd, periodKey := domain.CurrentUTCMonthPeriod(time.Now())
	out.PeriodKey = periodKey
	out.PeriodStart = periodStart
	out.PeriodEnd = periodEnd

	if monthlyLimit < 0 {
		out.Unlimited = true
		out.Remaining = -1
	}

	db, err := r.dbOrErr()
	if err != nil {
		return out, err
	}
	if userID == uuid.Nil {
		return out, fmt.Errorf("user id required")
	}
	if featureKey == "" {
		return out, fmt.Errorf("feature key required")
	}

	var row domain.UserUsage
	// Select only numeric/text columns for the hot path — avoids driver-specific
	// TIMESTAMP scan quirks when we only need usage_count.
	q := db.WithContext(ctx).
		Select("id", "user_id", "feature_key", "usage_count", "period_key").
		Where("user_id = ? AND feature_key = ? AND period_key = ?", userID, featureKey, periodKey).
		First(&row)
	if errors.Is(q.Error, gorm.ErrRecordNotFound) {
		out.UsageCount = 0
		if !out.Unlimited {
			out.Remaining = monthlyLimit
			if out.Remaining < 0 {
				out.Remaining = 0
			}
		}
		return out, nil
	}
	if q.Error != nil {
		return out, q.Error
	}

	out.UsageCount = row.UsageCount
	out.PeriodKey = row.PeriodKey
	if out.Unlimited {
		out.Remaining = -1
		return out, nil
	}
	remaining := monthlyLimit - row.UsageCount
	if remaining < 0 {
		remaining = 0
	}
	out.Remaining = remaining
	return out, nil
}

func (r *GormUserUsageRepository) ResetMonthlyUsage(
	ctx context.Context,
	cutoffPeriodEnd time.Time,
) (int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, err
	}
	if cutoffPeriodEnd.IsZero() {
		cutoffPeriodEnd, _, _ = domain.CurrentUTCMonthPeriod(time.Now())
	}

	// Prefer period_key < current month for portable cleanup; also match period_end.
	currentKey := cutoffPeriodEnd.UTC().Format("2006-01")
	res := db.WithContext(ctx).
		Where("period_key < ? OR period_end <= ?", currentKey, cutoffPeriodEnd.UTC()).
		Delete(&domain.UserUsage{})
	if res.Error != nil {
		return 0, res.Error
	}

	slog.Info("user_usage: monthly reset cleanup",
		"deleted_rows", res.RowsAffected,
		"before_period_key", currentKey,
		"cutoff_period_end", cutoffPeriodEnd.UTC().Format(time.RFC3339),
	)
	return res.RowsAffected, nil
}
