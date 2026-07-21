package domain

// Streak milestone day thresholds celebrated in the Progress UI.
//
// Free (basic): 3 + 7 day milestones only.
// Premium / Premium+ (full): the complete catalog.
var (
	BasicMilestoneDays = []int{3, 7}
	FullMilestoneDays  = []int{3, 7, 14, 30, 60, 100}
)

// MilestoneDef is one celebration threshold (mirrors frontend STREAK_MILESTONES).
type MilestoneDef struct {
	Days    int
	Tier    string // small | medium | large
	CopyKey string
}

// FullMilestoneCatalog is the ordered Premium catalog.
var FullMilestoneCatalog = []MilestoneDef{
	{Days: 3, Tier: "small", CopyKey: "d3"},
	{Days: 7, Tier: "small", CopyKey: "d7"},
	{Days: 14, Tier: "medium", CopyKey: "d14"},
	{Days: 30, Tier: "medium", CopyKey: "d30"},
	{Days: 60, Tier: "large", CopyKey: "d60"},
	{Days: 100, Tier: "large", CopyKey: "d100"},
}

// MilestoneDaysForPlan returns the celebration day list for a plan tier.
func MilestoneDaysForPlan(tier PlanTier) []int {
	defs := MilestoneCatalogForPlan(tier)
	out := make([]int, len(defs))
	for i, d := range defs {
		out[i] = d.Days
	}
	return out
}

// MilestoneCatalogForPlan returns celebration defs for a plan tier.
func MilestoneCatalogForPlan(tier PlanTier) []MilestoneDef {
	if NormalizePlanTier(tier).IsPaidPlan() {
		out := make([]MilestoneDef, len(FullMilestoneCatalog))
		copy(out, FullMilestoneCatalog)
		return out
	}
	out := make([]MilestoneDef, 0, len(BasicMilestoneDays))
	basic := map[int]struct{}{}
	for _, d := range BasicMilestoneDays {
		basic[d] = struct{}{}
	}
	for _, def := range FullMilestoneCatalog {
		if _, ok := basic[def.Days]; ok {
			out = append(out, def)
		}
	}
	return out
}
