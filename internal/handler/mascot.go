package handler

import (
	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	mascotuc "github.com/dadiary/backend/internal/usecase/mascot"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// MascotHandler serves GET /api/v1/me/mascot, PUT /api/v1/me/mascot/loadout,
// and POST /api/v1/me/mascot/glow/seen.
type MascotHandler struct {
	svc *mascotuc.Service
	cfg *config.Config
}

// NewMascotHandler constructs the handler.
func NewMascotHandler(svc *mascotuc.Service, cfg *config.Config) *MascotHandler {
	return &MascotHandler{svc: svc, cfg: cfg}
}

// allow reports whether the caller may use Bé Da.
// While domain.MascotOpenToEveryone is false, non-admins get 404 and the
// mascot service is not called. The email was already loaded by RequireAccessJWT,
// so this check does not query again. It uses config.IsAdminEmail, the same
// allow-list as RequireAdmin.
func (h *MascotHandler) allow(c *fiber.Ctx) (uuid.UUID, bool) {
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		_ = response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
		return uuid.Nil, false
	}
	if domain.MascotOpenToEveryone {
		return uid, true
	}
	if h != nil && h.cfg != nil && h.cfg.IsAdminEmail(middleware.UserEmailFromLocals(c)) {
		return uid, true
	}
	_ = response.Error(c, fiber.StatusNotFound, "not_found", "not found")
	return uuid.Nil, false
}

// Get handles GET /api/v1/me/mascot. The first allowed call creates the free default.
func (h *MascotHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid, ok := h.allow(c)
	if !ok {
		return nil
	}
	view, err := h.svc.Get(c.UserContext(), uid)
	if err != nil {
		return writeMascotErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, view)
}

// AckGlowSeen handles POST /api/v1/me/mascot/glow/seen.
// It sets glow_level_seen to the current level so level_up is true only once.
func (h *MascotHandler) AckGlowSeen(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid, ok := h.allow(c)
	if !ok {
		return nil
	}
	view, err := h.svc.AckGlowSeen(c.UserContext(), uid)
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
	uid, ok := h.allow(c)
	if !ok {
		return nil
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
