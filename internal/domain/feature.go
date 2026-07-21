package domain

// Feature identifies a product capability controlled by plan gating.
//
// Add new features here, then register entitlements in
// usecase/premium/entitlements.go — callers should never hard-code plan checks.
type Feature string

const (
	// FeatureAIRoutineSuggestion — POST /routines/suggest (monthly quota on Free).
	FeatureAIRoutineSuggestion Feature = "ai_routine_suggestion"
	// FeatureEditRoutine — structural routine saves (monthly quota on Free).
	FeatureEditRoutine Feature = "edit_routine"
	// FeatureWardrobeFull — create/edit wardrobe products (boolean).
	FeatureWardrobeFull Feature = "wardrobe_full"
	// FeatureProgressFullHistory — progress lookback window (months).
	FeatureProgressFullHistory Feature = "progress_full_history"
	// FeatureMilestoneFull — full milestone celebrations / history (boolean).
	FeatureMilestoneFull Feature = "milestone_full"
	// FeatureExportData — export diary / memory dumps (boolean).
	FeatureExportData Feature = "export_data"
	// FeatureAdvancedSkinAnalysis — multi-angle / deep analysis (Premium+).
	FeatureAdvancedSkinAnalysis Feature = "advanced_skin_analysis"
	// FeatureNoAds — hide promotional surfaces (boolean).
	FeatureNoAds Feature = "no_ads"
)

// AllFeatures is the canonical ordered list for catalog / API enumeration.
var AllFeatures = []Feature{
	FeatureAIRoutineSuggestion,
	FeatureEditRoutine,
	FeatureWardrobeFull,
	FeatureProgressFullHistory,
	FeatureMilestoneFull,
	FeatureExportData,
	FeatureAdvancedSkinAnalysis,
	FeatureNoAds,
}

// UsageFeatureFor maps a gated Feature onto a persisted UsageEvent key.
// Returns false for non-metered (boolean / history-window) features.
func UsageFeatureFor(f Feature) (UsageFeature, bool) {
	switch f {
	case FeatureAIRoutineSuggestion:
		return UsageRoutineSuggest, true
	case FeatureEditRoutine:
		return UsageRoutineManualEdit, true
	default:
		return "", false
	}
}
