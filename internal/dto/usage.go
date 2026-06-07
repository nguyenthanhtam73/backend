package dto

// UsageCounter is one monthly quota bucket for the free plan.
type UsageCounter struct {
	Used      int  `json:"used"`
	Limit     int  `json:"limit"`
	Remaining int  `json:"remaining"`
	Unlimited bool `json:"unlimited,omitempty"`
}

// WardrobeUsage describes wardrobe write access for the current plan.
type WardrobeUsage struct {
	CanWrite bool `json:"can_write"`
}

// UsageQuotaResponse is returned by GET /me/usage.
type UsageQuotaResponse struct {
	PlanTier          string         `json:"plan_tier"`
	IsPremium         bool           `json:"is_premium"`
	Period            string         `json:"period"` // YYYY-MM (UTC)
	Wardrobe          WardrobeUsage  `json:"wardrobe"`
	RoutineSuggest    UsageCounter   `json:"routine_suggest"`
	RoutineManualEdit UsageCounter   `json:"routine_manual_edit"`
}
