package handler

import (
	"context"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	"github.com/google/uuid"
)

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

func stripSkinCheckAds(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	res *dto.CreateSkinCheckResponse,
) {
	if res == nil || res.Analysis.Coach == nil {
		return
	}
	stripAdsIfEntitled(ctx, gates, userID, &res.Analysis.Coach.ProductSuggestions)
}

func stripSuggestAds(
	ctx context.Context,
	gates *premiumuc.Service,
	userID uuid.UUID,
	res *dto.SuggestJobStatusResponse,
) {
	if res == nil || res.Suggestion == nil {
		return
	}
	stripAdsIfEntitled(ctx, gates, userID, &res.Suggestion.ProductSuggestions)
}
