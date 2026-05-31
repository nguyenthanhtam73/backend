package handler

import (
	"errors"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	affiliateuc "github.com/dadiary/backend/internal/usecase/affiliate"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// AffiliateHandler serves affiliate analytics routes.
type AffiliateHandler struct {
	svc *affiliateuc.Service
}

// NewAffiliateHandler constructs AffiliateHandler.
func NewAffiliateHandler(svc *affiliateuc.Service) *AffiliateHandler {
	return &AffiliateHandler{svc: svc}
}

// LogClick handles POST /api/v1/affiliate/clicks.
func (h *AffiliateHandler) LogClick(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "affiliate unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body dto.LogAffiliateClickRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.LogClick(c.UserContext(), uid, body)
	if err != nil {
		if errors.Is(err, affiliateuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "affiliate_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusBadRequest, "invalid_click", err.Error())
	}
	return response.JSON(c, fiber.StatusCreated, res)
}
