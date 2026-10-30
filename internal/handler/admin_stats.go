package handler

import (
	"errors"
	"strings"

	adminstatsuc "github.com/dadiary/backend/internal/usecase/adminstats"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// AdminStatsHandler serves admin-only aggregate stats.
type AdminStatsHandler struct {
	svc *adminstatsuc.Service
}

// NewAdminStatsHandler constructs the handler.
func NewAdminStatsHandler(svc *adminstatsuc.Service) *AdminStatsHandler {
	return &AdminStatsHandler{svc: svc}
}

// RegistrationsBySource handles GET /api/v1/admin/stats/registrations-by-source.
// Query: since=YYYY-MM-DD (required), tz=IANA name (optional, default Asia/Ho_Chi_Minh).
// Must run after RequireAccessJWT and RequireAdmin.
func (h *AdminStatsHandler) RegistrationsBySource(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin registration stats unavailable")
	}
	out, err := h.svc.RegistrationsBySource(
		c.UserContext(),
		strings.TrimSpace(c.Query("since")),
		strings.TrimSpace(c.Query("tz")),
	)
	if err != nil {
		switch {
		case errors.Is(err, adminstatsuc.ErrUnavailable):
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
		case errors.Is(err, adminstatsuc.ErrInvalidSince):
			return response.Error(c, fiber.StatusBadRequest, "invalid_since", "since must be YYYY-MM-DD")
		case errors.Is(err, adminstatsuc.ErrInvalidTimezone):
			return response.Error(c, fiber.StatusBadRequest, "invalid_timezone", "tz must be an IANA name such as Asia/Ho_Chi_Minh")
		default:
			return response.Error(c, fiber.StatusInternalServerError, "registration_stats_error", "could not load registration stats")
		}
	}
	return response.JSON(c, fiber.StatusOK, out)
}
