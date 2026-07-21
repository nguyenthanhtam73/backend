package dto

// DashboardSummaryResponse is GET /api/v1/me/dashboard — home hero aggregate.
type DashboardSummaryResponse struct {
	PlanTier string `json:"plan_tier"`

	Streak  StreakResponse      `json:"streak"`
	Usage   UsageQuotaResponse  `json:"usage"`
	Progress DashboardProgress  `json:"progress"`
}

// DashboardProgress is a compact progress block for the dashboard.
type DashboardProgress struct {
	RangeDays int                 `json:"range_days"`
	From      string              `json:"from,omitempty"`
	To        string              `json:"to,omitempty"`
	Summary   ProgressSummaryData `json:"summary"`
}
