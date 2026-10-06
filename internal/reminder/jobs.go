// Package reminder is the single gate for outbound capture and check-in
// reminders.
//
// Every job that can nudge a user to check in must be listed in All and must
// call ExcludeMuted with its JobID on the candidate query. reminder_enabled
// NULL (never set) and true stay eligible. false is left out of the query.
// Transactional mail does not use this package.
//
// GET /me/check-in-reminder and ListDueUserIDs refresh flags. They are not
// outbound sends and must not call ExcludeMuted.
package reminder

// JobID names one outbound reminder. Values are stable for tests and logs.
type JobID string

const (
	// JobDailyPush is the 20:00 Asia/Ho_Chi_Minh daily_reminder push.
	JobDailyPush JobID = "daily_reminder_20_00"
	// JobStreakAtRisk is the 20:00 Asia/Ho_Chi_Minh streak_at_risk push.
	JobStreakAtRisk JobID = "streak_at_risk_20_00"
	// JobEveningEmail is the 19:30–21:30 D1 and Day-3 reminder email.
	JobEveningEmail JobID = "evening_email_d1_d3"
	// JobD0Email is the hourly same-day signup reminder email.
	JobD0Email JobID = "hourly_d0_email"
	// JobD0D1Push is the hourly d0_reminder and d1_reminder push.
	JobD0D1Push JobID = "hourly_d0_d1_push"
	// JobScheduledCapture sends one capture moment at the user's saved
	// local HH:MM. It replaces the fixed clocks for that user.
	JobScheduledCapture JobID = "scheduled_capture"
)

// Job is one outbound reminder the product can mute.
type Job struct {
	ID   JobID
	Name string
}

// All is the registry of outbound reminder jobs. A new job belongs here and
// in TestEveryReminderJobSkipsMutedUsers (runner map). That test fails when
// the two lists diverge, and when a listed job never calls ExcludeMuted.
func All() []Job {
	return []Job{
		{ID: JobDailyPush, Name: "20:00 daily_reminder push"},
		{ID: JobStreakAtRisk, Name: "20:00 streak_at_risk push"},
		{ID: JobEveningEmail, Name: "19:30–21:30 D1 and Day-3 email"},
		{ID: JobD0Email, Name: "hourly D0 email"},
		{ID: JobD0D1Push, Name: "hourly D0/D1 push"},
		{ID: JobScheduledCapture, Name: "per-user local capture reminder"},
	}
}

func registered(id JobID) bool {
	for _, job := range All() {
		if job.ID == id {
			return true
		}
	}
	return false
}
