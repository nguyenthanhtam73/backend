package handler

import (
	"errors"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	profileuc "github.com/dadiary/backend/internal/usecase/profile"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// ProfileHandler serves skin profile and onboarding endpoints.
type ProfileHandler struct {
	svc *profileuc.Service
}

// NewProfileHandler constructs ProfileHandler.
func NewProfileHandler(svc *profileuc.Service) *ProfileHandler {
	return &ProfileHandler{svc: svc}
}

// GetSkin handles GET /profile/skin.
func (h *ProfileHandler) GetSkin(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.svc.GetSkin(c.UserContext(), uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "profile_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// PutSkin handles PUT /profile/skin (manual edits, no AI).
func (h *ProfileHandler) PutSkin(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body dto.PutSkinProfileRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.PutSkin(c.UserContext(), uid, body)
	if err != nil {
		return mapProfileError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// CompleteOnboarding handles POST /profile/onboarding/complete.
func (h *ProfileHandler) CompleteOnboarding(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body dto.OnboardingCompleteRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.CompleteOnboarding(c.UserContext(), uid, body)
	if err != nil {
		return mapProfileError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// PreviewOnboardingComplete handles POST /onboarding/preview-complete (guest trial, no DB write).
func (h *ProfileHandler) PreviewOnboardingComplete(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	var body dto.OnboardingCompleteRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	starter, err := h.svc.PreviewOnboardingComplete(c.UserContext(), body)
	if err != nil {
		return mapProfileError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, dto.OnboardingPreviewResponse{StarterRoutine: starter})
}

func mapProfileError(c *fiber.Ctx, err error) error {
	if errors.Is(err, profileuc.ErrInvalidInput) {
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
	}
	return response.Error(c, fiber.StatusInternalServerError, "profile_error", err.Error())
}
