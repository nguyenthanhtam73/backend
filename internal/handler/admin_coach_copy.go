package handler

import (
	"errors"
	"strconv"
	"strings"

	coachcopyaudituc "github.com/dadiary/backend/internal/usecase/coachcopyaudit"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// AdminCoachCopyHandler serves GET /api/v1/admin/coach-copy-audit.
type AdminCoachCopyHandler struct {
	svc *coachcopyaudituc.Service
}

// NewAdminCoachCopyHandler constructs the handler.
func NewAdminCoachCopyHandler(svc *coachcopyaudituc.Service) *AdminCoachCopyHandler {
	return &AdminCoachCopyHandler{svc: svc}
}

// Get handles GET /api/v1/admin/coach-copy-audit?days=7.
// Must run after RequireAccessJWT and RequireAdmin. The body is counts only.
func (h *AdminCoachCopyHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "coach copy audit unavailable")
	}
	days, err := parseAuditDays(c.Query("days"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_days", "days must be a whole number from 1 to 90")
	}
	out, err := h.svc.Audit(c.UserContext(), days)
	if err != nil {
		switch {
		case errors.Is(err, coachcopyaudituc.ErrUnavailable):
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
		case errors.Is(err, coachcopyaudituc.ErrInvalidDays):
			return response.Error(c, fiber.StatusBadRequest, "invalid_days", "days must be a whole number from 1 to 90")
		default:
			return response.Error(c, fiber.StatusInternalServerError, "coach_copy_audit_error", "could not load coach copy audit")
		}
	}
	return response.JSON(c, fiber.StatusOK, out)
}

func parseAuditDays(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 7, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 90 {
		return 0, coachcopyaudituc.ErrInvalidDays
	}
	return n, nil
}
