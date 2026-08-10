package handler

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
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

// stripOnboardingAnalyzeAds previously cleared affiliate fields for Premium no_ads.
// Product affiliate intros are intentionally kept for Premium (same as Free).
func stripOnboardingAnalyzeAds(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	res *dto.OnboardingSkinAnalyzeResponse,
	locale string,
) {
	_ = ctx
	_ = gates
	_ = userID
	_ = res
	_ = locale
}

// stripOnboardingGuidanceAds previously cleared affiliate fields for Premium no_ads.
// Product affiliate intros are intentionally kept for Premium (same as Free).
func stripOnboardingGuidanceAds(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	res *dto.OnboardingProductGuidanceResponse,
	locale string,
) {
	_ = ctx
	_ = gates
	_ = userID
	_ = res
	_ = locale
}

func stripSkinCheckAds(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	res *dto.CreateSkinCheckResponse,
	locale string,
) {
	// Product affiliate intros stay for Premium (same as Free).
	_ = ctx
	_ = gates
	_ = userID
	_ = res
	_ = locale
}

func stripSuggestAds(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	res *dto.SuggestJobStatusResponse,
	locale string,
) {
	// Product affiliate intros stay for Premium (same as Free).
	_ = ctx
	_ = gates
	_ = userID
	_ = res
	_ = locale
}

func userHasNoAds(ctx context.Context, gates *premiumuc.Service, userID uuid.UUID) bool {
	if gates == nil || userID == uuid.Nil {
		return false
	}
	ok, _, err := gates.CanUseFeature(ctx, userID, domain.FeatureNoAds)
	return err == nil && ok
}

// stripStarterRoutineAdsIfEntitled is a no-op: product affiliate intros stay for Premium.
func stripStarterRoutineAdsIfEntitled(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	routine *dto.StarterRoutineResponse,
	locale string,
) {
	_ = ctx
	_ = gates
	_ = userID
	_ = routine
	_ = locale
}

// stripOnboardingSnapshotAdsIfEntitled is a no-op: product affiliate intros stay for Premium.
func stripOnboardingSnapshotAdsIfEntitled(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	snap *json.RawMessage,
	locale string,
) {
	_ = ctx
	_ = gates
	_ = userID
	_ = snap
	_ = locale
}
