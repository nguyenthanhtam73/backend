package dto

// AdminFunnelStatsResponse is GET /api/v1/admin/funnel-stats.
//
// Read-only leaky-bucket proxies from Postgres. Paywall impressions are
// client-only and are not persisted — PaywallViews is always null.
type AdminFunnelStatsResponse struct {
	// SignedUp1d / SignedUp7d count non-deleted users created in a rolling window.
	SignedUp1d int64 `json:"signed_up_1d"`
	SignedUp7d int64 `json:"signed_up_7d"`

	// SkinCheckUsers* are distinct users with ≥1 non-deleted skin_check
	// (ever, or created in the rolling 1d / 7d window).
	SkinCheckUsersEver int64 `json:"skin_check_users_ever"`
	SkinCheckUsers1d   int64 `json:"skin_check_users_1d"`
	SkinCheckUsers7d   int64 `json:"skin_check_users_7d"`

	// D0CheckinUsers is users with a skin_check.check_date on their Vietnam signup day.
	D0CheckinUsers int64 `json:"d0_checkin_users"`
	// D0CheckinUsers7d is the same proxy among users who signed up in the last 7d.
	D0CheckinUsers7d int64 `json:"d0_checkin_users_7d"`

	// D1CheckinUsers is users with a skin_check on the Vietnam day after signup.
	// Only users whose signup day is at least one VN day ago are eligible.
	D1CheckinUsers    int64 `json:"d1_checkin_users"`
	D1EligibleUsers   int64 `json:"d1_eligible_users"`
	D1CheckinUsers7d  int64 `json:"d1_checkin_users_7d"`
	D1EligibleUsers7d int64 `json:"d1_eligible_users_7d"`

	// PaidOrders7d is payment_orders with status=paid in the last 7d
	// (paid_at when set, otherwise created_at).
	PaidOrders7d int64 `json:"paid_orders_7d"`

	// PaywallViews is always null — the paywall is client-only.
	PaywallViews *int64           `json:"paywall_views"`
	Notes        AdminFunnelNotes `json:"notes"`
	// AsOf is the UTC timestamp when stats were computed.
	AsOf string `json:"as_of"`
}

// AdminFunnelNotes explains proxies that are missing or calendar-scoped.
type AdminFunnelNotes struct {
	Paywall  string `json:"paywall"`
	Calendar string `json:"calendar"`
	D0       string `json:"d0"`
	D1       string `json:"d1"`
	Windows  string `json:"windows"`
}
