package domain

import "time"

// PushJobLock persists once-per-run claims so multiple API replicas (or a
// restart after the send window) cannot fan-out twice.
//
// JobName is the primary key (e.g. "daily_reminder", "streak_at_risk").
// LastRunDate is the claim key: a Vietnam civil day "2006-01-02" (10 chars)
// or an hour key "2006-01-02-15" (13 chars, D0/D1 hourly job). Empty means
// unlocked / available to claim. Column is VARCHAR(16) — hour keys do not
// fit in the original VARCHAR(10) and Postgres rejects the INSERT (SQLSTATE 22001).
//
// ExpiresAt is a lease deadline: if the owning pod crashes mid-fan-out, another
// replica may steal the claim after expiry (see PushJobLeaseDuration).
type PushJobLock struct {
	JobName     string    `gorm:"primaryKey;size:64" json:"job_name"`
	LastRunDate string    `gorm:"not null;size:16;default:''" json:"last_run_date"`
	ClaimedAt   time.Time `json:"claimed_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PushJobRunKey layouts written to LastRunDate.
const (
	PushJobDayKeyLayout  = "2006-01-02"    // 10 chars — evening / daily jobs
	PushJobHourKeyLayout = "2006-01-02-15" // 13 chars — D0/D1 hourly job
	PushJobRunKeyMaxLen  = 16
)

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
	// PushJobD0D1Reminder is the hourly D0/D1 email+push delivery claim.
	PushJobD0D1Reminder = "d0_d1_reminder"
)
