package domain

import (
	"time"

	"github.com/google/uuid"
)

// CaptureReminderClaim is one capture-reminder moment for a user on their
// local civil date (YYYY-MM-DD in ReminderTimezone, or Asia/Ho_Chi_Minh when
// that zone is missing or invalid).
//
// The primary key is the durable "at most one moment" lock shared by every
// outbound capture job. scheduled_capture inserts the row before it sends.
// A second tick or a second replica gets no row and does not send. Fixed-clock
// jobs do not insert it; they leave saved schedules out of their candidate
// queries so they cannot take a second moment the same local day.
type CaptureReminderClaim struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey" json:"user_id"`
	LocalDate string    `gorm:"primaryKey;size:10" json:"local_date"`
	ClaimedAt time.Time `gorm:"not null" json:"claimed_at"`
}

func (CaptureReminderClaim) TableName() string {
	return "capture_reminder_claims"
}
