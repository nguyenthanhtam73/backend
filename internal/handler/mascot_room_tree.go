package handler

import (
	"bytes"
	"errors"
	"strings"

	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/usecase/roomtree"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// AttachTree enables the shared-tree routes.
func (h *MascotRoomHandler) AttachTree(tree *roomtree.Service) {
	if h != nil {
		h.tree = tree
	}
}

func (h *MascotRoomHandler) treeOr(c *fiber.Ctx) (*roomtree.Service, uuid.UUID, bool) {
	uid, ok := h.allow(c)
	if !ok {
		return nil, uuid.Nil, false
	}
	if h == nil || h.tree == nil {
		_ = response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
		return nil, uuid.Nil, false
	}
	return h.tree, uid, true
}

// TreeCatalog is GET /api/v1/mascot/tree-types.
// It lists enabled types. creatable is true only for leaf.
func (h *MascotRoomHandler) TreeCatalog(c *fiber.Ctx) error {
	tree, _, ok := h.treeOr(c)
	if !ok {
		return nil
	}
	body, err := tree.Catalog(c.UserContext())
	if err != nil {
		return writeTreeErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, body)
}

// Tree is GET /api/v1/mascot/rooms/:code/tree.
func (h *MascotRoomHandler) Tree(c *fiber.Ctx) error {
	tree, uid, ok := h.treeOr(c)
	if !ok {
		return nil
	}
	state, err := tree.State(c.UserContext(), uid, c.Params("code"))
	if err != nil {
		return writeTreeErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, state)
}

type treeWaterBody struct {
	IdempotencyKey string `json:"idempotency_key"`
}

// WaterTree is POST /api/v1/mascot/rooms/:code/tree/water.
// The body may be empty or {"idempotency_key":"..."}.
func (h *MascotRoomHandler) WaterTree(c *fiber.Ctx) error {
	tree, uid, ok := h.treeOr(c)
	if !ok {
		return nil
	}
	key, err := idempotencyFromWater(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	state, err := tree.Water(c.UserContext(), uid, c.Params("code"), key)
	if err != nil {
		return writeTreeErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, state)
}

type treeSeenBody struct {
	Stages []int `json:"stages"`
}

// MarkTreeRewardsSeen is POST /api/v1/mascot/rooms/:code/tree/rewards/seen.
// The body may be empty, which marks every unseen reward for this member,
// or {"stages":[2,3]}.
func (h *MascotRoomHandler) MarkTreeRewardsSeen(c *fiber.Ctx) error {
	tree, uid, ok := h.treeOr(c)
	if !ok {
		return nil
	}
	stages, err := stagesFromSeen(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	state, err := tree.MarkRewardsSeen(c.UserContext(), uid, c.Params("code"), stages)
	if err != nil {
		return writeTreeErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, state)
}

type createRoomBody struct {
	TreeType string `json:"tree_type"`
}

func treeTypeFromCreate(c *fiber.Ctx) (string, error) {
	if c == nil || len(bytes.TrimSpace(c.Body())) == 0 {
		return "", nil
	}
	var body createRoomBody
	if err := decodeStrict(c.Body(), &body); err != nil {
		return "", err
	}
	return strings.TrimSpace(body.TreeType), nil
}

func stagesFromSeen(c *fiber.Ctx) ([]int, error) {
	if c == nil || len(bytes.TrimSpace(c.Body())) == 0 {
		return nil, nil
	}
	var body treeSeenBody
	if err := decodeStrict(c.Body(), &body); err != nil {
		return nil, err
	}
	return body.Stages, nil
}

func idempotencyFromWater(c *fiber.Ctx) (string, error) {
	if c == nil || len(bytes.TrimSpace(c.Body())) == 0 {
		return "", nil
	}
	var body treeWaterBody
	if err := decodeStrict(c.Body(), &body); err != nil {
		return "", err
	}
	return strings.TrimSpace(body.IdempotencyKey), nil
}

func writeTreeErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, repository.ErrMascotRoomNotFound):
		return response.Error(c, fiber.StatusNotFound, "room_not_found", "room not found")
	case errors.Is(err, repository.ErrMascotRoomBadCode):
		return response.Error(c, fiber.StatusBadRequest, "bad_code", "invite code is not valid")
	case errors.Is(err, repository.ErrMascotRoomEnded):
		return response.Error(c, fiber.StatusConflict, "room_ended", "this room has ended")
	case errors.Is(err, repository.ErrMascotRoomNotMember):
		return response.Error(c, fiber.StatusForbidden, "not_a_member", "not a room member")
	case errors.Is(err, repository.ErrTreeNotReal):
		return response.Error(c, fiber.StatusForbidden, "not_real_member", err.Error())
	case errors.Is(err, repository.ErrTreeNeedsSkinCheck):
		return response.Error(c, fiber.StatusConflict, "needs_skin_check", err.Error())
	case errors.Is(err, repository.ErrTreeAlreadyWatered):
		return response.Error(c, fiber.StatusConflict, "already_watered", err.Error())
	case errors.Is(err, repository.ErrTreeBloomed):
		return response.Error(c, fiber.StatusConflict, "bloomed", err.Error())
	case errors.Is(err, repository.ErrTreeTypeUnknown):
		return response.Error(c, fiber.StatusBadRequest, "bad_tree_type", err.Error())
	case errors.Is(err, repository.ErrTreeTypeUnavailable):
		return response.Error(c, fiber.StatusBadRequest, "tree_type_unavailable", err.Error())
	case errors.Is(err, repository.ErrTreeBadIdempotency):
		return response.Error(c, fiber.StatusBadRequest, "bad_idempotency_key", err.Error())
	default:
		return response.Error(c, fiber.StatusServiceUnavailable, "database_unavailable", "database is not available")
	}
}
