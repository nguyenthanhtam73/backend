package premium

import "github.com/dadiary/backend/internal/domain"

// EntitlementKind classifies how a feature is gated for a plan.
type EntitlementKind string

const (
	// EntitlementBoolean — on/off (wardrobe, export, ads, …).
	EntitlementBoolean EntitlementKind = "boolean"
	// EntitlementMonthlyQuota — N uses per UTC calendar month; -1 = unlimited.
	EntitlementMonthlyQuota EntitlementKind = "monthly_quota"
	// EntitlementHistoryMonths — lookback window in months; 0 = unlimited / all time.
	EntitlementHistoryMonths EntitlementKind = "history_months"
)

// Entitlement is one cell in the plan × feature matrix.
type Entitlement struct {
	Kind EntitlementKind
	// Enabled is used for EntitlementBoolean (and as a quick deny for others).
	Enabled bool
	// MonthlyLimit is for EntitlementMonthlyQuota. -1 means unlimited.
	MonthlyLimit int
	// HistoryMonths is for EntitlementHistoryMonths. 0 means unlimited history.
	HistoryMonths int
}

// UnlimitedMonthly marks a metered feature as uncapped for the plan.
const UnlimitedMonthly = -1

// catalog is the single source of truth for Free / Premium / Premium+ gates.
//
// To add a feature:
//  1. Add a domain.Feature constant.
//  2. Fill three rows below (free / premium / premium_plus).
//  3. Call CanUseFeature / GetRemainingQuota from the relevant handler.
var catalog = map[domain.PlanTier]map[domain.Feature]Entitlement{
	domain.PlanFree: {
		domain.FeatureAIRoutineSuggestion:  {Kind: EntitlementMonthlyQuota, Enabled: true, MonthlyLimit: 3},
		domain.FeatureEditRoutine:          {Kind: EntitlementMonthlyQuota, Enabled: true, MonthlyLimit: 5},
		domain.FeatureWardrobeFull:         {Kind: EntitlementBoolean, Enabled: false},
		domain.FeatureProgressFullHistory:  {Kind: EntitlementHistoryMonths, Enabled: true, HistoryMonths: 3},
		domain.FeatureMilestoneFull:        {Kind: EntitlementBoolean, Enabled: false},
		domain.FeatureExportData:           {Kind: EntitlementBoolean, Enabled: false},
		domain.FeatureAdvancedSkinAnalysis: {Kind: EntitlementBoolean, Enabled: false},
		domain.FeatureNoAds:                {Kind: EntitlementBoolean, Enabled: false},
	},
	domain.PlanPremium: {
		domain.FeatureAIRoutineSuggestion:  {Kind: EntitlementMonthlyQuota, Enabled: true, MonthlyLimit: UnlimitedMonthly},
		domain.FeatureEditRoutine:          {Kind: EntitlementMonthlyQuota, Enabled: true, MonthlyLimit: UnlimitedMonthly},
		domain.FeatureWardrobeFull:         {Kind: EntitlementBoolean, Enabled: true},
		domain.FeatureProgressFullHistory:  {Kind: EntitlementHistoryMonths, Enabled: true, HistoryMonths: 12},
		domain.FeatureMilestoneFull:        {Kind: EntitlementBoolean, Enabled: true},
		domain.FeatureExportData:           {Kind: EntitlementBoolean, Enabled: true},
		domain.FeatureAdvancedSkinAnalysis: {Kind: EntitlementBoolean, Enabled: false},
		domain.FeatureNoAds:                {Kind: EntitlementBoolean, Enabled: true},
	},
	domain.PlanPremiumPlus: {
		domain.FeatureAIRoutineSuggestion:  {Kind: EntitlementMonthlyQuota, Enabled: true, MonthlyLimit: UnlimitedMonthly},
		domain.FeatureEditRoutine:          {Kind: EntitlementMonthlyQuota, Enabled: true, MonthlyLimit: UnlimitedMonthly},
		domain.FeatureWardrobeFull:         {Kind: EntitlementBoolean, Enabled: true},
		domain.FeatureProgressFullHistory:  {Kind: EntitlementHistoryMonths, Enabled: true, HistoryMonths: 0}, // all time
		domain.FeatureMilestoneFull:        {Kind: EntitlementBoolean, Enabled: true},
		domain.FeatureExportData:           {Kind: EntitlementBoolean, Enabled: true},
		domain.FeatureAdvancedSkinAnalysis: {Kind: EntitlementBoolean, Enabled: true}, // multi-angle, deep memory, early access
		domain.FeatureNoAds:                {Kind: EntitlementBoolean, Enabled: true},
	},
}

// EntitlementFor returns the entitlement for a plan × feature pair.
// Unknown tiers fall back to Free; unknown features are denied.
func EntitlementFor(tier domain.PlanTier, feature domain.Feature) Entitlement {
	tier = domain.NormalizePlanTier(tier)
	byFeature, ok := catalog[tier]
	if !ok {
		byFeature = catalog[domain.PlanFree]
	}
	ent, ok := byFeature[feature]
	if !ok {
		return Entitlement{Kind: EntitlementBoolean, Enabled: false}
	}
	return ent
}
