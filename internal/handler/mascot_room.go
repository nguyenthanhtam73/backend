package handler

import (
	"context"
	"errors"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/lobby"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// liveRooms drops a socket when someone leaves a room over REST.
type liveRooms interface {
	LeaveLive(userID uuid.UUID, code string)
}

// MascotRoomHandler is the REST API for an 8-person invite room.
type MascotRoomHandler struct {
	repo *repository.MascotRoomRepository
	cfg  *config.Config
	live liveRooms
}

// NewMascotRoomHandler constructs the room routes.
func NewMascotRoomHandler(repo *repository.MascotRoomRepository, cfg *config.Config, live liveRooms) *MascotRoomHandler {
	return &MascotRoomHandler{repo: repo, cfg: cfg, live: live}
}

// mascotRoomCreatesPerMinute is the per-account cap on POST /mascot/rooms.
const mascotRoomCreatesPerMinute = 5

// Register attaches create, get, join, and leave under /api/v1.
func (h *MascotRoomHandler) Register(r fiber.Router, jwt fiber.Handler) {
	if jwt == nil {
		return
	}
	r.Post("/mascot/rooms", jwt, middleware.AILimiter(mascotRoomCreatesPerMinute, time.Minute), h.Create)
	r.Get("/mascot/rooms/:code", jwt, h.Get)
	r.Post("/mascot/rooms/:code/join", jwt, h.Join)
	r.Post("/mascot/rooms/:code/leave", jwt, h.Leave)
}

func (h *MascotRoomHandler) Create(c *fiber.Ctx) error {
	if h == nil || h.repo == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid, ok := h.allow(c)
	if !ok {
		return nil
	}
	view, err := h.repo.Create(c.UserContext(), uid)
	if err != nil {
		return writeRoomErr(c, err)
	}
	return response.JSON(c, fiber.StatusCreated, roomJSON(view))
}

func (h *MascotRoomHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.repo == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid, ok := h.allow(c)
	if !ok {
		return nil
	}
	view, err := h.repo.Get(c.UserContext(), uid, c.Params("code"))
	if err != nil {
		return writeRoomErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, roomJSON(view))
}

func (h *MascotRoomHandler) Join(c *fiber.Ctx) error {
	if h == nil || h.repo == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid, ok := h.allow(c)
	if !ok {
		return nil
	}
	view, err := h.repo.Join(c.UserContext(), uid, c.Params("code"))
	if err != nil {
		return writeRoomErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, roomJSON(view))
}

func (h *MascotRoomHandler) Leave(c *fiber.Ctx) error {
	if h == nil || h.repo == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid, ok := h.allow(c)
	if !ok {
		return nil
	}
	code := c.Params("code")
	if err := h.repo.Leave(c.UserContext(), uid, code); err != nil {
		return writeRoomErr(c, err)
	}
	if h.live != nil {
		h.live.LeaveLive(uid, code)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"left": true})
}

func (h *MascotRoomHandler) allow(c *fiber.Ctx) (uuid.UUID, bool) {
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

func roomJSON(view repository.MascotRoomView) fiber.Map {
	body := fiber.Map{
		"code":         view.Code,
		"cap":          view.Cap,
		"member_count": view.MemberCount,
		"joined":       view.Joined,
		"owner":        view.Owner,
		"invite_url":   lobby.RoomInvite(view.Code),
	}
	if !view.CreatedAt.IsZero() {
		body["created_at"] = view.CreatedAt.UTC().Format(time.RFC3339)
	}
	return body
}

func writeRoomErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, repository.ErrMascotRoomNotFound):
		return response.Error(c, fiber.StatusNotFound, "room_not_found", "room not found")
	case errors.Is(err, repository.ErrMascotRoomFull):
		return response.Error(c, fiber.StatusConflict, "room_full", "room is full")
	case errors.Is(err, repository.ErrMascotRoomBadCode):
		return response.Error(c, fiber.StatusBadRequest, "bad_code", "invite code is not valid")
	case errors.Is(err, repository.ErrMascotRoomTooMany):
		return response.Error(c, fiber.StatusConflict, "too_many_rooms", "too many open rooms")
	default:
		return response.Error(c, fiber.StatusServiceUnavailable, "database_unavailable", "database is not available")
	}
}

// mascotRoomGate adapts the room table to the lobby socket.
// It does not log the account id.
type mascotRoomGate struct {
	repo *repository.MascotRoomRepository
}

func newMascotRoomGate(repo *repository.MascotRoomRepository) lobby.RoomGate {
	return mascotRoomGate{repo: repo}
}

func (g mascotRoomGate) Enter(userID uuid.UUID, code string) error {
	if g.repo == nil {
		return errors.New("room store unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := g.repo.CanEnter(ctx, userID, code)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrMascotRoomNotFound):
		return lobby.ErrRoomNotFound
	case errors.Is(err, repository.ErrMascotRoomNotMember):
		return lobby.ErrNotRoomMember
	case errors.Is(err, repository.ErrMascotRoomBadCode):
		return lobby.ErrRoomNotFound
	default:
		return err
	}
}
