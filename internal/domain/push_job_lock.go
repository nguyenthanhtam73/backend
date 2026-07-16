package domain

import "time"

// PushJobLock persists once-per-day claims for evening push jobs so multiple
// API replicas (or a restart after the send window) cannot fan-out twice.
//
// JobName is the primary key (e.g. "daily_reminder", "streak_at_risk").
// LastRunDate is the Vietnam civil day "2006-01-02" that currently holds the
// claim; empty means unlocked / available to claim.
//
// ExpiresAt is a lease deadline: if the owning pod crashes mid-fan-out, another
// replica may steal the claim after expiry (see PushJobLeaseDuration).
type PushJobLock struct {
	JobName     string    `gorm:"primaryKey;size:64" json:"job_name"`
	LastRunDate string    `gorm:"not null;size:10;default:''" json:"last_run_date"`
	ClaimedAt   time.Time `json:"claimed_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PushJobLeaseDuration is how long a claim is held before another pod may steal
// it. Long enough for a full evening fan-out; short enough to recover from crash.
const PushJobLeaseDuration = 45 * time.Minute

func (PushJobLock) TableName() string {
	return "push_job_locks"
}

// Known push job lock names (must stay stable — used as PK).
const (
	PushJobDailyReminder = "daily_reminder"
	PushJobStreakAtRisk  = "streak_at_risk"
)
