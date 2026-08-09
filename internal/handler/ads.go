package handler

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/service/ai"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	"github.com/google/uuid"
)

// uiLocaleFromClimateJSON reads climate_context.ui_locale ("en"|"vi").
func uiLocaleFromClimateJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return ""
	}
	v, ok := m["ui_locale"].(string)
	if !ok {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "en", "vi":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

// stripAdsIfEntitled clears product_suggestions when the user has FeatureNoAds.
// Fail-open: if the gate is unavailable or errors, suggestions stay (Free path).
func stripAdsIfEntitled(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	suggestions *[]dto.ProductSuggestion,
) {
	if gates == nil || suggestions == nil || len(*suggestions) == 0 || userID == uuid.Nil {
		return
	}
	ok, _, err := gates.CanUseFeature(ctx, userID, domain.FeatureNoAds)
	if err != nil || !ok {
		return
	}
	*suggestions = nil
}

// stripOnboardingAnalyzeAds clears affiliate commerce fields on analyze-skin for Premium no_ads.
// Generic product_guidance copy is kept (roles / why / benefits) without Shopee links.
func stripOnboardingAnalyzeAds(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	res *dto.OnboardingSkinAnalyzeResponse,
	locale string,
) {
	if gates == nil || res == nil || userID == uuid.Nil {
		return
	}
	ok, _, err := gates.CanUseFeature(ctx, userID, domain.FeatureNoAds)
	if err != nil || !ok {
		return
	}
	res.ProductSuggestions = nil
	res.ProductGuidance = ai.StripAffiliateFromProductGuidance(res.ProductGuidance, locale)
}

func stripSkinCheckAds(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	res *dto.CreateSkinCheckResponse,
	locale string,
) {
	if res == nil || res.Analysis.Coach == nil {
		return
	}
	stripAdsIfEntitled(ctx, gates, userID, &res.Analysis.Coach.ProductSuggestions)
	if gates == nil || userID == uuid.Nil {
		return
	}
	ok, _, err := gates.CanUseFeature(ctx, userID, domain.FeatureNoAds)
	if err != nil || !ok {
		return
	}
	res.Analysis.Coach.ProductGuidance = ai.StripAffiliateFromProductGuidance(res.Analysis.Coach.ProductGuidance, locale)
}

func stripSuggestAds(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	res *dto.SuggestJobStatusResponse,
	locale string,
) {
	if res == nil || res.Suggestion == nil {
		return
	}
	stripAdsIfEntitled(ctx, gates, userID, &res.Suggestion.ProductSuggestions)
	if gates == nil || userID == uuid.Nil {
		return
	}
	ok, _, err := gates.CanUseFeature(ctx, userID, domain.FeatureNoAds)
	if err != nil || !ok {
		return
	}
	res.Suggestion.ProductGuidance = ai.StripAffiliateFromProductGuidance(res.Suggestion.ProductGuidance, locale)
}

func userHasNoAds(ctx context.Context, gates *premiumuc.Service, userID uuid.UUID) bool {
	if gates == nil || userID == uuid.Nil {
		return false
	}
	ok, _, err := gates.CanUseFeature(ctx, userID, domain.FeatureNoAds)
	return err == nil && ok
}

// stripStarterRoutineAdsIfEntitled clears suggestions + affiliate fields on guidance for Premium.
func stripStarterRoutineAdsIfEntitled(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	routine *dto.StarterRoutineResponse,
	locale string,
) {
	if routine == nil || !userHasNoAds(ctx, gates, userID) {
		return
	}
	routine.ProductSuggestions = nil
	routine.ProductGuidance = ai.StripAffiliateFromProductGuidance(routine.ProductGuidance, locale)
}

// stripOnboardingSnapshotAdsIfEntitled clears commerce in starter_routine + skin_analysis
// after GetSkin enrich — Premium must never receive branded affiliate JSON.
func stripOnboardingSnapshotAdsIfEntitled(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	snap *json.RawMessage,
	locale string,
) {
	if snap == nil || len(*snap) == 0 || !userHasNoAds(ctx, gates, userID) {
		return
	}
	*snap = ai.StripAffiliateFromOnboardingSnapshot(*snap, locale)
}
