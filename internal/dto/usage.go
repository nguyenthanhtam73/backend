package dto

// UsageCounter is one monthly quota bucket.
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

// FeatureAccessDTO is one entry in the plan feature catalog (GET /me/usage).
type FeatureAccessDTO struct {
	Allowed       bool   `json:"allowed"`
	Unlimited     bool   `json:"unlimited,omitempty"`
	Used          int    `json:"used,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	Remaining     int    `json:"remaining,omitempty"`
	Kind          string `json:"kind,omitempty"`           // boolean | monthly_quota | history_months
	HistoryMonths int    `json:"history_months,omitempty"` // progress lookback; 0 = all when allowed
}

// UsageQuotaResponse is returned by GET /me/usage.
type UsageQuotaResponse struct {
	PlanTier              string                      `json:"plan_tier"`
	IsPremium             bool                        `json:"is_premium"`      // Premium or Premium+
	IsPremiumPlus         bool                        `json:"is_premium_plus"` // top tier only
	Period                string                      `json:"period"`          // YYYY-MM (UTC)
	Wardrobe              WardrobeUsage               `json:"wardrobe"`
	RoutineSuggest        UsageCounter                `json:"routine_suggest"`
	RoutineManualEdit     UsageCounter                `json:"routine_manual_edit"`
	ProgressHistoryMonths int                         `json:"progress_history_months"` // 0 = unlimited
	Features              map[string]FeatureAccessDTO `json:"features,omitempty"`
}
