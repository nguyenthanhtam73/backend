package handler

import (
	"errors"

	adminfunneluc "github.com/dadiary/backend/internal/usecase/adminfunnel"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// AdminFunnelHandler serves GET /api/v1/admin/funnel-stats.
type AdminFunnelHandler struct {
	svc *adminfunneluc.Service
}

// NewAdminFunnelHandler constructs the handler.
func NewAdminFunnelHandler(svc *adminfunneluc.Service) *AdminFunnelHandler {
	return &AdminFunnelHandler{svc: svc}
}

// Get handles GET /api/v1/admin/funnel-stats (admin JWT).
func (h *AdminFunnelHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin funnel stats unavailable")
	}
	out, err := h.svc.Stats(c.UserContext())
	if err != nil {
		if errors.Is(err, adminfunneluc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "funnel_stats_error", "could not load funnel stats")
	}
	return response.JSON(c, fiber.StatusOK, out)
}
