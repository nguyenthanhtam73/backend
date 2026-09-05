package handler

import (
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	checkinreminderuc "github.com/dadiary/backend/internal/usecase/checkinreminder"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// CheckInReminderHandler serves D0/D1 reminder state for the current user
// and an admin refresh of the snapshot table.
type CheckInReminderHandler struct {
	svc *checkinreminderuc.Service
}

// NewCheckInReminderHandler constructs the handler.
func NewCheckInReminderHandler(svc *checkinreminderuc.Service) *CheckInReminderHandler {
	return &CheckInReminderHandler{svc: svc}
}

// Get handles GET /api/v1/me/check-in-reminder.
func (h *CheckInReminderHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "reminder unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.svc.GetForUser(c.UserContext(), uid)
	if err != nil {
		if ae, ok := domain.AsAppError(err); ok {
			return response.Error(c, ae.HTTPStatus, ae.Code, ae.Message)
		}
		return response.Error(c, fiber.StatusInternalServerError, "reminder_error", "could not load reminder")
	}
	return response.JSONWithMessage(c, fiber.StatusOK, res, "ok")
}

// AdminRefresh handles POST /api/v1/admin/check-in-reminders/refresh.
func (h *CheckInReminderHandler) AdminRefresh(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "reminder unavailable")
	}
	res, err := h.svc.RefreshWindow(c.UserContext())
	if err != nil {
		if ae, ok := domain.AsAppError(err); ok {
			return response.Error(c, ae.HTTPStatus, ae.Code, ae.Message)
		}
		return response.Error(c, fiber.StatusInternalServerError, "reminder_refresh_failed", "could not refresh reminders")
	}
	return response.JSONWithMessage(c, fiber.StatusOK, res, "checkin_reminders_refreshed")
}
