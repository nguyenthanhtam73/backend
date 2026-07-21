package dto

// MilestoneCatalogResponse is GET /api/v1/me/streak/milestones.
type MilestoneCatalogResponse struct {
	PlanTier     string             `json:"plan_tier"`
	FullAccess   bool               `json:"full_access"` // milestone_full entitlement
	MilestoneDays []int             `json:"milestone_days"`
	Items        []MilestoneItemDTO `json:"items"`
}

// MilestoneItemDTO describes one celebration threshold.
type MilestoneItemDTO struct {
	Days    int    `json:"days"`
	Tier    string `json:"tier"` // small | medium | large
	CopyKey string `json:"copy_key"`
}
