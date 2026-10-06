package handler

import (
	"errors"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	reminderprefsuc "github.com/dadiary/backend/internal/usecase/reminderprefs"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// ReminderPrefsHandler serves GET/PUT /api/v1/me/reminder.
type ReminderPrefsHandler struct {
	svc *reminderprefsuc.Service
}

// NewReminderPrefsHandler constructs the handler.
func NewReminderPrefsHandler(svc *reminderprefsuc.Service) *ReminderPrefsHandler {
	return &ReminderPrefsHandler{svc: svc}
}

// Get handles GET /api/v1/me/reminder.
func (h *ReminderPrefsHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "reminder unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	view, err := h.svc.Get(c.UserContext(), uid)
	if err != nil {
		return writeReminderErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, view)
}

type reminderPrefsBody struct {
	PushOptInAction string `json:"push_opt_in_action"`
}

// Put handles PUT /api/v1/me/reminder.
func (h *ReminderPrefsHandler) Put(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "reminder unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body reminderPrefsBody
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	view, err := h.svc.Apply(c.UserContext(), uid, body.PushOptInAction)
	if err != nil {
		return writeReminderErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, view)
}

func writeReminderErr(c *fiber.Ctx, err error) error {
	if errors.Is(err, reminderprefsuc.ErrInvalidAction) {
		return response.Error(c, fiber.StatusBadRequest, "invalid_action", "push_opt_in_action must be skip_push_opt_in or consume_push_opt_in_reshow")
	}
	if errors.Is(err, reminderprefsuc.ErrUnavailable) {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "reminder unavailable")
	}
	if ae, ok := domain.AsAppError(err); ok {
		return response.Error(c, ae.HTTPStatus, ae.Code, ae.Message)
	}
	return response.Error(c, fiber.StatusInternalServerError, "reminder_error", "could not update reminder")
}
