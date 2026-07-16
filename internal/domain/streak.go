package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Streak tracks consecutive calendar days with at least one SkinCheck
// (Asia/Ho_Chi_Minh civil dates, stored as date-only).
// One row per user; UserID is the primary key.
type Streak struct {
	UserID           uuid.UUID  `gorm:"type:uuid;primaryKey" json:"user_id"`
	CurrentStreak    int        `gorm:"not null;default:0" json:"current_streak"`
	LongestStreak    int        `gorm:"not null;default:0" json:"longest_streak"`
	LastCheckInDate  *time.Time `gorm:"type:date" json:"last_check_in_date,omitempty"`
	ProtectedUntil   *time.Time `gorm:"type:date" json:"protected_until,omitempty"` // day covered by an *active* freeze
	// LastFreezeDate is the calendar day most recently covered by a freeze.
	LastFreezeDate *time.Time `gorm:"type:date" json:"last_freeze_date,omitempty"`
	// FreezeDates is a JSON array of YYYY-MM-DD freeze-covered days (newest last),
	// capped for mini-history across the recent window.
	FreezeDates      json.RawMessage `gorm:"type:jsonb" json:"freeze_dates,omitempty"`
	FreezesAvailable int             `gorm:"not null;default:1" json:"freezes_available"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (Streak) TableName() string {
	return "streaks"
}

// DefaultFreezesAvailable is granted when a streak row is first created.
const DefaultFreezesAvailable = 1

// MaxFreezeDatesKept limits persisted freeze history for mini-history ( ~2 months).
const MaxFreezeDatesKept = 60
