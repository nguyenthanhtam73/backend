package handler

import (
	"errors"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	funneleventuc "github.com/dadiary/backend/internal/usecase/funnelevent"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// FunnelEventHandler serves POST /api/v1/funnel-events.
type FunnelEventHandler struct {
	svc *funneleventuc.Service
}

// NewFunnelEventHandler constructs the handler.
func NewFunnelEventHandler(svc *funneleventuc.Service) *FunnelEventHandler {
	return &FunnelEventHandler{svc: svc}
}

// Log handles POST /api/v1/funnel-events.
// Auth is optional: a valid access token attaches user_id; everyone else is stored as a guest.
// The response is 204 with an empty body.
func (h *FunnelEventHandler) Log(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "funnel events unavailable")
	}
	if c.Request().Header.ContentLength() > domain.MaxFunnelBodyBytes || len(c.Body()) > domain.MaxFunnelBodyBytes {
		return response.Error(c, fiber.StatusBadRequest, "invalid_funnel_event", "body is too large")
	}
	var body dto.LogFunnelEventRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	err := h.svc.Log(c.UserContext(), middleware.UserIDFromLocals(c), c.Get("User-Agent"), body)
	if err != nil {
		var invalid *funneleventuc.ValidationError
		if errors.As(err, &invalid) {
			return response.Error(c, fiber.StatusBadRequest, "invalid_funnel_event", invalid.Error())
		}
		if errors.Is(err, funneleventuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "funnel_event_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "funnel_event_error", "could not record event")
	}
	return c.SendStatus(fiber.StatusNoContent)
}
