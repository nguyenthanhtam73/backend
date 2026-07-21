package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserUsage is one metered feature counter for a single UTC billing period.
//
// Periods are calendar months in UTC:
//   - period_key   = "2006-01" (stable lookup key; unique with user+feature)
//   - period_start = 1st 00:00 UTC
//   - period_end   = 1st of next month 00:00 UTC
//
// Concurrent increments use a transaction + compare-and-set on usage_count.
type UserUsage struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID     uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_user_usages_user_feature_period,priority:1" json:"user_id"`
	FeatureKey string    `gorm:"size:64;not null;uniqueIndex:idx_user_usages_user_feature_period,priority:2" json:"feature_key"`
	UsageCount int       `gorm:"not null;default:0" json:"usage_count"`
	// PeriodKey is YYYY-MM (UTC) — preferred equality key (portable across drivers).
	PeriodKey   string    `gorm:"size:7;not null;uniqueIndex:idx_user_usages_user_feature_period,priority:3" json:"period_key"`
	PeriodStart time.Time `gorm:"not null" json:"period_start"`
	PeriodEnd   time.Time `gorm:"not null;index:idx_user_usages_period_end" json:"period_end"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (UserUsage) TableName() string {
	return "user_usages"
}

func (u *UserUsage) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	if u.PeriodKey == "" && !u.PeriodStart.IsZero() {
		u.PeriodKey = u.PeriodStart.UTC().Format("2006-01")
	}
	return nil
}

// CurrentUTCMonthPeriod returns [start, end) and period_key for the UTC month containing now.
func CurrentUTCMonthPeriod(now time.Time) (start, end time.Time, key string) {
	now = now.UTC()
	start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end = start.AddDate(0, 1, 0)
	key = start.Format("2006-01")
	return start, end, key
}

// IsMeteredFeature reports whether feature_key should be tracked in user_usages.
func IsMeteredFeature(featureKey string) bool {
	switch Feature(featureKey) {
	case FeatureAIRoutineSuggestion, FeatureEditRoutine:
		return true
	default:
		return false
	}
}
