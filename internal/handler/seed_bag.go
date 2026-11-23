package handler

import (
	"bytes"
	"errors"

	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// SeedBag is GET /api/v1/mascot/seed-bag.
func (h *MascotRoomHandler) SeedBag(c *fiber.Ctx) error {
	tree, uid, ok := h.treeOr(c)
	if !ok {
		return nil
	}
	bag, err := tree.Bag(c.UserContext(), uid)
	if err != nil {
		return writeSeedErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, bag)
}

type seedTradeBody struct {
	Offer          []seedOffer `json:"offer"`
	Want           string      `json:"want"`
	IdempotencyKey string      `json:"idempotency_key"`
}

type seedOffer struct {
	TypeKey string `json:"type_key"`
	Count   int    `json:"count"`
}

// TradeShards is POST /api/v1/mascot/seed-bag/trade.
func (h *MascotRoomHandler) TradeShards(c *fiber.Ctx) error {
	tree, uid, ok := h.treeOr(c)
	if !ok {
		return nil
	}
	var body seedTradeBody
	if len(bytes.TrimSpace(c.Body())) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	if err := decodeStrict(c.Body(), &body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	offer := make([]repository.BagStack, len(body.Offer))
	for i, row := range body.Offer {
		offer[i] = repository.BagStack{TypeKey: row.TypeKey, Count: row.Count}
	}
	bag, err := tree.Trade(c.UserContext(), uid, offer, body.Want, body.IdempotencyKey)
	if err != nil {
		return writeSeedErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, bag)
}

type seedBuyBody struct {
	TypeKey        string `json:"type_key"`
	IdempotencyKey string `json:"idempotency_key"`
}

// BuyShard is POST /api/v1/mascot/seed-bag/buy.
func (h *MascotRoomHandler) BuyShard(c *fiber.Ctx) error {
	tree, uid, ok := h.treeOr(c)
	if !ok {
		return nil
	}
	var body seedBuyBody
	if len(bytes.TrimSpace(c.Body())) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	if err := decodeStrict(c.Body(), &body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	bag, err := tree.Buy(c.UserContext(), uid, body.TypeKey, body.IdempotencyKey)
	if err != nil {
		return writeSeedErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, bag)
}

func writeSeedErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, repository.ErrNotEnoughCoins):
		return response.Error(c, fiber.StatusConflict, "not_enough_coins", err.Error())
	case errors.Is(err, repository.ErrNotEnoughShards):
		return response.Error(c, fiber.StatusConflict, "not_enough_shards", err.Error())
	case errors.Is(err, repository.ErrNeedSeed):
		return response.Error(c, fiber.StatusConflict, "need_seed", err.Error())
	case errors.Is(err, repository.ErrMysteryNotTradable):
		return response.Error(c, fiber.StatusBadRequest, "mystery_not_tradable", err.Error())
	case errors.Is(err, repository.ErrMysteryNotForSale):
		return response.Error(c, fiber.StatusBadRequest, "mystery_not_for_sale", err.Error())
	case errors.Is(err, repository.ErrBadShardType):
		return response.Error(c, fiber.StatusBadRequest, "bad_shard_type", err.Error())
	case errors.Is(err, repository.ErrBadTrade):
		return response.Error(c, fiber.StatusBadRequest, "bad_trade", err.Error())
	case errors.Is(err, repository.ErrSameShardType):
		return response.Error(c, fiber.StatusBadRequest, "same_shard_type", err.Error())
	default:
		return writeTreeErr(c, err)
	}
}
