package dto

// CheckInReminderResponse is GET /api/v1/me/check-in-reminder.
// The frontend polls this to show a D0/D1 first-check-in nudge.
type CheckInReminderResponse struct {
	Kind            string                  `json:"kind"` // d0 | d1 | none
	Due             bool                    `json:"due"`
	SignupDate      string                  `json:"signup_date,omitempty"` // YYYY-MM-DD (VN)
	DaysSinceSignup int                     `json:"days_since_signup"`
	CheckedInToday  bool                    `json:"checked_in_today"`
	Channels        CheckInReminderChannels `json:"channels"`
}

// CheckInReminderChannels tells the client which delivery paths exist today.
// Email is false until an ESP is wired. Push is the existing evening
// daily_reminder job (not a D0/D1-specific send).
type CheckInReminderChannels struct {
	InApp bool `json:"in_app"`
	Email bool `json:"email"`
	// PushEvening is true when VAPID keys are configured so the existing
	// 20:00 VN daily_reminder job can send to subscribed devices.
	PushEvening bool `json:"push_evening"`
	// PushD0D1Specific is reserved; this release does not send a separate
	// D0/D1 push (avoid a second evening notification).
	PushD0D1Specific bool `json:"push_d0_d1_specific"`
	// EmailReason / PushNote are machine-stable so FE can hide or explain.
	EmailReason string `json:"email_reason,omitempty"`
	PushNote    string `json:"push_note,omitempty"`
}

// CheckInReminderRefreshResponse is POST /api/v1/admin/check-in-reminders/refresh.
type CheckInReminderRefreshResponse struct {
	Scanned  int `json:"scanned"`
	DueD0    int `json:"due_d0"`
	DueD1    int `json:"due_d1"`
	Cleared  int `json:"cleared"`
	Upserted int `json:"upserted"`
}

// ExpirePendingOrdersResponse is POST /api/v1/admin/payments/expire-pending.
type ExpirePendingOrdersResponse struct {
	TTLHours           int    `json:"ttl_hours"`
	Cutoff             string `json:"cutoff"`
	Expired            int64  `json:"expired"`
	PendingFresh       int64  `json:"pending_fresh"`
	PendingStaleBefore int64  `json:"pending_stale_before,omitempty"`
}
