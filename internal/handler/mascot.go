package handler

import (
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	mascotuc "github.com/dadiary/backend/internal/usecase/mascot"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// MascotHandler serves GET /api/v1/me/mascot, PUT /api/v1/me/mascot/loadout,
// POST /api/v1/me/mascot/glow/seen, POST /api/v1/me/mascot/intro,
// DELETE /api/v1/me/mascot/intro, and POST /api/v1/mascot/daily-quest/claim.
type MascotHandler struct {
	svc      *mascotuc.Service
	cfg      *config.Config
	shop     *repository.XuShopRepository
	quests   *repository.DailyQuestRepository
	questNow func() time.Time
}

// AttachShop sells regular pieces for xu. The same Bé Da gate covers it.
func (h *MascotHandler) AttachShop(shop *repository.XuShopRepository) {
	if h != nil {
		h.shop = shop
	}
}

// AttachDailyQuest credits the private-room morning and evening chests and
// adds daily_quest on GET /api/v1/me/mascot. Nil leaves that field off.
func (h *MascotHandler) AttachDailyQuest(quests *repository.DailyQuestRepository) {
	if h != nil {
		h.quests = quests
	}
}

// NewMascotHandler constructs the handler.
func NewMascotHandler(svc *mascotuc.Service, cfg *config.Config) *MascotHandler {
	return &MascotHandler{svc: svc, cfg: cfg}
}

// allow reports whether the caller may use Bé Da.
// While domain.MascotOpenToEveryone is false, a signed-in caller is allowed
// when they are on the admin allow-list or the request is the Android trial
// client (X-DaDiary-Client: android-app, including the embedded WebView).
// Everyone else gets 404 and the mascot service is not called. The email was
// already loaded by RequireAccessJWT, so this check does not query again.
func (h *MascotHandler) allow(c *fiber.Ctx) (uuid.UUID, bool) {
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		_ = response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
		return uuid.Nil, false
	}
	var cfg *config.Config
	if h != nil {
		cfg = h.cfg
	}
	if mascotOpen(c, cfg) {
		return uid, true
	}
	_ = response.Error(c, fiber.StatusNotFound, "not_found", "not found")
	return uuid.Nil, false
}

// allowAdmin is the intro-reset gate. A signed-in admin may continue.
// Anyone else, including the Android trial client, gets 404 so the route
// stays hidden. The email was already loaded by RequireAccessJWT.
func (h *MascotHandler) allowAdmin(c *fiber.Ctx) (uuid.UUID, bool) {
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		_ = response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
		return uuid.Nil, false
	}
	var cfg *config.Config
	if h != nil {
		cfg = h.cfg
	}
	if mascotAdmin(c, cfg) {
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
	if h.quests != nil {
		status, err := h.quests.Status(c.UserContext(), uid, h.questClock())
		if err != nil {
			return writeMascotErr(c, err)
		}
		view.DailyQuest = &dto.DailyQuestStatus{
			Morning: dto.DailyQuestSession{Done: status.Morning.Done, Claimed: status.Morning.Claimed},
			Evening: dto.DailyQuestSession{Done: status.Evening.Done, Claimed: status.Evening.Claimed},
			Amounts: dto.DailyQuestAmountView(),
		}
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

// SaveIntro handles POST /api/v1/me/mascot/intro.
// Body is {"gender":"female"} or {"gender":"male"}.
func (h *MascotHandler) SaveIntro(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid, ok := h.allow(c)
	if !ok {
		return nil
	}
	var body struct {
		Gender string `json:"gender"`
	}
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	view, err := h.svc.SaveIntro(c.UserContext(), uid, body.Gender)
	if err != nil {
		return writeMascotErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, view)
}

// ResetIntro handles DELETE /api/v1/me/mascot/intro.
// It clears the caller's intro_seen_at so the Nam/Nữ picker shows again.
// Gender and the rest of the row stay. No row is created when there is none.
func (h *MascotHandler) ResetIntro(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid, ok := h.allowAdmin(c)
	if !ok {
		return nil
	}
	if err := h.svc.ResetIntro(c.UserContext(), uid); err != nil {
		return writeMascotErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, dto.MascotIntroReset{IntroSeen: false})
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
	view, err := h.svc.UpdateLoadout(c.UserContext(), uid, c.Body())
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
