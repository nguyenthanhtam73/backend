package handler

import (
	"strings"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// Guards a public, unauthenticated endpoint against oversized payloads.
const maxGuidanceConcerns = 12

// ProductGuidance handles POST /api/v1/onboarding/product-guidance.
//
// Same commerce output as analyze-skin, but derived from onboarding answers only,
// so users who skip the photo step still get product roles and affiliate CTAs.
// Pure catalog matching — no model call, hence no AI rate limiter.
func (h *OnboardingAnalyzeHandler) ProductGuidance(c *fiber.Ctx) error {
	if h == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "configuration missing")
	}
	var req dto.OnboardingProductGuidanceRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "expected a JSON object")
	}

	locale := "vi"
	if strings.EqualFold(strings.TrimSpace(req.Locale), "en") {
		locale = "en"
	}
	concerns := req.Concerns
	if len(concerns) > maxGuidanceConcerns {
		concerns = concerns[:maxGuidanceConcerns]
	}

	guidance, suggestions := ai.BuildManualProductGuidance(
		strings.TrimSpace(req.Goal),
		strings.TrimSpace(req.SkinType),
		concerns,
		locale,
	)
	res := dto.OnboardingProductGuidanceResponse{
		Phase:              ai.PhaseCalmFirst,
		ProductGuidance:    guidance,
		ProductSuggestions: suggestions,
	}
	stripOnboardingGuidanceAds(c.UserContext(), h.premium, middleware.UserIDFromLocals(c), &res, locale)
	return response.JSON(c, fiber.StatusOK, res)
}
