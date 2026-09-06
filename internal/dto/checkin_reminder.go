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
// Email is true only when Resend (RESEND_API_KEY + EMAIL_FROM) is configured.
// PushD0D1Specific is true when the check-in reminder job is enabled.
type CheckInReminderChannels struct {
	InApp bool `json:"in_app"`
	Email bool `json:"email"`
	// PushEvening is true when VAPID keys are configured so the existing
	// 20:00 VN daily_reminder job can send to subscribed devices.
	PushEvening bool `json:"push_evening"`
	// PushD0D1Specific is true when the D0/D1 fan-out job is enabled.
	PushD0D1Specific bool `json:"push_d0_d1_specific"`
	// EmailReason / PushNote are machine-stable so FE can hide or explain.
	EmailReason string `json:"email_reason,omitempty"`
	PushNote    string `json:"push_note,omitempty"`
}

// CheckInReminderDeliveryStats is the outbound fan-out from refresh+deliver.
type CheckInReminderDeliveryStats struct {
	EmailSent    int `json:"email_sent"`
	EmailSkipped int `json:"email_skipped"`
	EmailFailed  int `json:"email_failed"`
	PushSent     int `json:"push_sent"`
	PushSkipped  int `json:"push_skipped"`
	PushFailed   int `json:"push_failed"`
	Candidates   int `json:"candidates"`
}

// CheckInReminderRefreshResponse is POST /api/v1/admin/check-in-reminders/refresh.
type CheckInReminderRefreshResponse struct {
	Scanned  int                          `json:"scanned"`
	DueD0    int                          `json:"due_d0"`
	DueD1    int                          `json:"due_d1"`
	Cleared  int                          `json:"cleared"`
	Upserted int                          `json:"upserted"`
	Delivery CheckInReminderDeliveryStats `json:"delivery,omitempty"`
}

// ExpirePendingOrdersResponse is POST /api/v1/admin/payments/expire-pending.
type ExpirePendingOrdersResponse struct {
	TTLHours           int    `json:"ttl_hours"`
	Cutoff             string `json:"cutoff"`
	Expired            int64  `json:"expired"`
	PendingFresh       int64  `json:"pending_fresh"`
	PendingStaleBefore int64  `json:"pending_stale_before,omitempty"`
}
