package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/usecase/roomquest"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// AttachQuest enables the shared quest, today board, and nudge routes.
func (h *MascotRoomHandler) AttachQuest(quest *roomquest.Service) {
	if h != nil {
		h.quest = quest
	}
}

func (h *MascotRoomHandler) questOr(c *fiber.Ctx) (*roomquest.Service, uuid.UUID, bool) {
	uid, ok := h.allow(c)
	if !ok {
		return nil, uuid.Nil, false
	}
	if h == nil || h.quest == nil {
		_ = response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
		return nil, uuid.Nil, false
	}
	return h.quest, uid, true
}

// Quest is GET /api/v1/mascot/rooms/:code/quest.
func (h *MascotRoomHandler) Quest(c *fiber.Ctx) error {
	quest, uid, ok := h.questOr(c)
	if !ok {
		return nil
	}
	state, err := quest.State(c.UserContext(), uid, c.Params("code"))
	if err != nil {
		return writeQuestErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, state)
}

// Today is GET /api/v1/mascot/rooms/:code/today.
func (h *MascotRoomHandler) Today(c *fiber.Ctx) error {
	quest, uid, ok := h.questOr(c)
	if !ok {
		return nil
	}
	board, err := quest.Today(c.UserContext(), uid, c.Params("code"))
	if err != nil {
		return writeQuestErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, board)
}

type questClaimBody struct {
	MemberDays int `json:"member_days"`
}

// Claim is POST /api/v1/mascot/rooms/:code/quest/claim.
// The body is {"member_days": N}. Any other field is rejected.
func (h *MascotRoomHandler) Claim(c *fiber.Ctx) error {
	quest, uid, ok := h.questOr(c)
	if !ok {
		return nil
	}
	var body questClaimBody
	if err := decodeStrict(c.Body(), &body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	got, err := quest.Claim(c.UserContext(), uid, c.Params("code"), body.MemberDays)
	if err != nil {
		return writeQuestErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, got)
}

type questNudgeBody struct {
	UserID string `json:"user_id"`
}

// Nudge is POST /api/v1/mascot/rooms/:code/nudge.
// The body is {"user_id": "..."}. There is no message field.
func (h *MascotRoomHandler) Nudge(c *fiber.Ctx) error {
	quest, uid, ok := h.questOr(c)
	if !ok {
		return nil
	}
	var body questNudgeBody
	if err := decodeStrict(c.Body(), &body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	target, err := uuid.Parse(body.UserID)
	if err != nil || target == uuid.Nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_user", "user id must be a valid UUID")
	}
	got, err := quest.Nudge(c.UserContext(), uid, c.Params("code"), target)
	if err != nil {
		return writeQuestErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, got)
}

func decodeStrict(raw []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing json")
	}
	return nil
}

func writeQuestErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, repository.ErrMascotRoomNotFound):
		return response.Error(c, fiber.StatusNotFound, "room_not_found", "room not found")
	case errors.Is(err, repository.ErrMascotRoomBadCode):
		return response.Error(c, fiber.StatusBadRequest, "bad_code", "invite code is not valid")
	case errors.Is(err, repository.ErrMascotRoomEnded):
		return response.Error(c, fiber.StatusConflict, "room_ended", "this room has ended")
	case errors.Is(err, repository.ErrMascotRoomNotMember):
		return response.Error(c, fiber.StatusForbidden, "not_a_member", "not a room member")
	case errors.Is(err, repository.ErrQuestUnknownChest):
		return response.Error(c, fiber.StatusBadRequest, "unknown_chest", "unknown chest")
	case errors.Is(err, repository.ErrQuestChestClosed):
		return response.Error(c, fiber.StatusConflict, "chest_closed", "chest is not open yet")
	case errors.Is(err, repository.ErrNudgeSelf):
		return response.Error(c, fiber.StatusBadRequest, "nudge_self", "you cannot nudge yourself")
	case errors.Is(err, repository.ErrNudgeTarget):
		return response.Error(c, fiber.StatusNotFound, "not_in_room", "that person is not in this room")
	case errors.Is(err, repository.ErrNudgeDone):
		return response.Error(c, fiber.StatusConflict, "already_done", "this friend already cared or checked in today")
	case errors.Is(err, repository.ErrNudgeAlready):
		return response.Error(c, fiber.StatusConflict, "already_nudged", "you already nudged this friend today")
	default:
		return response.Error(c, fiber.StatusServiceUnavailable, "database_unavailable", "database is not available")
	}
}
