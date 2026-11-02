package handler

import (
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	mascotuc "github.com/dadiary/backend/internal/usecase/mascot"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// MascotHandler serves GET /api/v1/me/mascot and PUT /api/v1/me/mascot/loadout.
type MascotHandler struct {
	svc *mascotuc.Service
}

// NewMascotHandler constructs the handler.
func NewMascotHandler(svc *mascotuc.Service) *MascotHandler {
	return &MascotHandler{svc: svc}
}

// Get handles GET /api/v1/me/mascot. The first call creates the free default.
func (h *MascotHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	view, err := h.svc.Get(c.UserContext(), uid)
	if err != nil {
		return writeMascotErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, view)
}

// PutLoadout handles PUT /api/v1/me/mascot/loadout.
func (h *MascotHandler) PutLoadout(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body domain.MascotLoadout
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	view, err := h.svc.UpdateLoadout(c.UserContext(), uid, body)
	if err != nil {
		return writeMascotErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, view)
}

func writeMascotErr(c *fiber.Ctx, err error) error {
	if ae, ok := domain.AsAppError(err); ok {
		return response.Error(c, ae.HTTPStatus, ae.Code, ae.Message)
	}
	return response.Error(c, fiber.StatusInternalServerError, "mascot_error", "could not load mascot")
}
