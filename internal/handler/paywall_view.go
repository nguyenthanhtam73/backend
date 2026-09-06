package handler

import (
	"errors"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	paywallviewuc "github.com/dadiary/backend/internal/usecase/paywallview"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// PaywallViewHandler serves POST /api/v1/analytics/paywall-view.
type PaywallViewHandler struct {
	svc *paywallviewuc.Service
}

// NewPaywallViewHandler constructs the handler.
func NewPaywallViewHandler(svc *paywallviewuc.Service) *PaywallViewHandler {
	return &PaywallViewHandler{svc: svc}
}

// Log handles POST /api/v1/analytics/paywall-view (JWT optional — guests OK).
func (h *PaywallViewHandler) Log(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "paywall view unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	var body dto.LogPaywallViewRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
		}
	}
	res, err := h.svc.Log(c.UserContext(), uid, body)
	if err != nil {
		if errors.Is(err, paywallviewuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "paywall_view_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusBadRequest, "invalid_paywall_view", err.Error())
	}
	return response.JSON(c, fiber.StatusCreated, res)
}
