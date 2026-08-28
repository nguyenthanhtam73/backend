package handler

import (
	"errors"
	"strings"

	adminactivityuc "github.com/dadiary/backend/internal/usecase/adminactivity"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// AdminActivityHandler serves GET /api/v1/admin/activity.
type AdminActivityHandler struct {
	svc *adminactivityuc.Service
}

// NewAdminActivityHandler constructs the handler.
func NewAdminActivityHandler(svc *adminactivityuc.Service) *AdminActivityHandler {
	return &AdminActivityHandler{svc: svc}
}

// Get handles GET /api/v1/admin/activity?date=YYYY-MM-DD
func (h *AdminActivityHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin activity unavailable")
	}
	res, err := h.svc.ForDate(c.UserContext(), strings.TrimSpace(c.Query("date")))
	if err != nil {
		switch {
		case errors.Is(err, adminactivityuc.ErrUnavailable):
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
		case errors.Is(err, adminactivityuc.ErrInvalidInput):
			return response.Error(c, fiber.StatusBadRequest, "invalid_date", "date must be YYYY-MM-DD")
		default:
			return response.Error(c, fiber.StatusInternalServerError, "admin_activity_error", "could not load activity")
		}
	}
	return response.JSON(c, fiber.StatusOK, res)
}
