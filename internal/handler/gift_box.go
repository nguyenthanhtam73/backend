package handler

import (
	"strings"

	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// GiftBoxes is GET /api/v1/mascot/gift-boxes.
// The list is unopened boxes only. Boxes are not sold.
func (h *MascotRoomHandler) GiftBoxes(c *fiber.Ctx) error {
	tree, uid, ok := h.treeOr(c)
	if !ok {
		return nil
	}
	body, err := tree.GiftBoxes(c.UserContext(), uid)
	if err != nil {
		return writeTreeErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, body)
}

// GiftBoxRates is GET /api/v1/mascot/gift-boxes/rates.
// Query tree_type limits the table to one tree. The rates are fixed.
func (h *MascotRoomHandler) GiftBoxRates(c *fiber.Ctx) error {
	tree, _, ok := h.treeOr(c)
	if !ok {
		return nil
	}
	body, err := tree.GiftRates(c.UserContext(), strings.TrimSpace(c.Query("tree_type")))
	if err != nil {
		return writeTreeErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, body)
}

// OpenGiftBox is POST /api/v1/mascot/gift-boxes/:id/open.
// The box id is the idempotency key. A second call returns the first prize.
func (h *MascotRoomHandler) OpenGiftBox(c *fiber.Ctx) error {
	tree, uid, ok := h.treeOr(c)
	if !ok {
		return nil
	}
	body, err := tree.OpenGiftBox(c.UserContext(), uid, c.Params("id"))
	if err != nil {
		return writeTreeErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, body)
}
