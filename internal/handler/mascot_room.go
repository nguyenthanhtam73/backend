package handler

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/lobby"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/usecase/roomcare"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// liveRooms drops a socket when someone leaves a room over REST and
// copies a refreshed care streak onto live seats.
type liveRooms interface {
	LeaveLive(userID uuid.UUID, code string)
	NoteCareStreaks(code string, streaks map[uuid.UUID]int)
}

// MascotRoomHandler is the REST API for an 8-person invite room.
// Members leave themselves. The creator cannot kick, ban, or remove anyone.
type MascotRoomHandler struct {
	repo *repository.MascotRoomRepository
	cfg  *config.Config
	live liveRooms
	care *roomcare.Cache
}

// NewMascotRoomHandler constructs the room routes.
func NewMascotRoomHandler(repo *repository.MascotRoomRepository, cfg *config.Config, live liveRooms, care *roomcare.Cache) *MascotRoomHandler {
	return &MascotRoomHandler{repo: repo, cfg: cfg, live: live, care: care}
}

// mascotRoomCreatesPerMinute is the per-account cap on POST /mascot/rooms.
const mascotRoomCreatesPerMinute = 5

// Register attaches create, mine, get, join, and leave under /api/v1.
// /mine is registered before /:code so the list is not read as a room code.
func (h *MascotRoomHandler) Register(r fiber.Router, jwt fiber.Handler) {
	if jwt == nil {
		return
	}
	r.Post("/mascot/rooms", jwt, middleware.AILimiter(mascotRoomCreatesPerMinute, time.Minute), h.Create)
	r.Get("/mascot/rooms/mine", jwt, h.Mine)
	r.Get("/mascot/rooms/:code/board", jwt, h.Board)
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
	return h.writeRoom(c, fiber.StatusCreated, view)
}

// Mine is GET /api/v1/mascot/rooms/mine.
// Logged-out callers get 401 from the JWT middleware. The list is the open
// rooms this account owns or still belongs to. Each row has the room code,
// member count, and capacity. Closed rooms are omitted.
func (h *MascotRoomHandler) Mine(c *fiber.Ctx) error {
	if h == nil || h.repo == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid, ok := h.allow(c)
	if !ok {
		return nil
	}
	rooms, err := h.repo.ListOpenForUser(c.UserContext(), uid)
	if err != nil {
		return writeRoomErr(c, err)
	}
	body := make([]fiber.Map, 0, len(rooms))
	for _, view := range rooms {
		body = append(body, roomJSON(view))
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"rooms": body})
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
	return h.writeRoom(c, fiber.StatusOK, view)
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
	return h.writeRoom(c, fiber.StatusOK, view)
}

// Board is GET /api/v1/mascot/rooms/:code/board.
// Members only. Sorted by care_streak descending, then nickname ascending.
// Each row is nickname and care_streak.
func (h *MascotRoomHandler) Board(c *fiber.Ctx) error {
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
	if !view.Joined {
		return response.Error(c, fiber.StatusForbidden, "not_a_member", "not a room member")
	}
	members, streaks, err := h.memberCare(c.UserContext(), view)
	if err != nil {
		return writeRoomErr(c, err)
	}
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].CareStreak != members[j].CareStreak {
			return members[i].CareStreak > members[j].CareStreak
		}
		return members[i].Nickname < members[j].Nickname
	})
	if h.live != nil {
		h.live.NoteCareStreaks(view.Code, streaks)
	}
	return response.JSON(c, fiber.StatusOK, dto.RoomBoardResponse{Members: members})
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

func (h *MascotRoomHandler) writeRoom(c *fiber.Ctx, status int, view repository.MascotRoomView) error {
	members, streaks, err := h.memberCare(c.UserContext(), view)
	if err != nil {
		return writeRoomErr(c, err)
	}
	if h.live != nil && view.Joined {
		h.live.NoteCareStreaks(view.Code, streaks)
	}
	body := roomJSON(view)
	body["members"] = members
	return response.JSON(c, status, body)
}

// memberCare is the join-ordered roster. Non-members get an empty list and
// no nicknames. Streaks come from the per-user per-day cache.
func (h *MascotRoomHandler) memberCare(ctx context.Context, view repository.MascotRoomView) ([]dto.RoomMemberCare, map[uuid.UUID]int, error) {
	if !view.Joined || h == nil || h.repo == nil {
		return []dto.RoomMemberCare{}, nil, nil
	}
	rows, err := h.repo.ListMembers(ctx, view.Code)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.UserID
	}
	streaks := map[uuid.UUID]int{}
	if h.care != nil {
		streaks = h.care.Refresh(ctx, ids)
	}
	out := make([]dto.RoomMemberCare, len(rows))
	for i, row := range rows {
		out[i] = dto.RoomMemberCare{Nickname: row.Nickname, CareStreak: streaks[row.UserID]}
	}
	return out, streaks, nil
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
