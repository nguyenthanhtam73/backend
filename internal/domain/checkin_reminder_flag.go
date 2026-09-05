package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CheckInReminderFlag is the last computed D0/D1 reminder state for one user.
// The GET /me/check-in-reminder path recomputes live and upserts this row so
// the frontend and a later email/push fan-out share one snapshot.
type CheckInReminderFlag struct {
	UserID          uuid.UUID      `gorm:"type:uuid;primaryKey" json:"user_id"`
	Kind            string         `gorm:"size:8;not null;default:none;index" json:"kind"`
	Due             bool           `gorm:"not null;default:false;index" json:"due"`
	SignupDate      time.Time      `gorm:"type:date;not null" json:"signup_date"`
	CheckedInToday  bool           `gorm:"not null;default:false" json:"checked_in_today"`
	DaysSinceSignup int            `gorm:"not null;default:0" json:"days_since_signup"`
	ComputedOn      time.Time      `gorm:"type:date;not null;index" json:"computed_on"`
	ComputedAt      time.Time      `json:"computed_at"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (CheckInReminderFlag) TableName() string {
	return "checkin_reminder_flags"
}
