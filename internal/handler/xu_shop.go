package handler

import (
	"errors"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/pkg/response"
	"github.com/dadiary/backend/pkg/usermsg"
	"github.com/gofiber/fiber/v2"
)

type shopBuyBody struct {
	IdempotencyKey string `json:"idempotency_key"`
	ItemID         string `json:"item_id"`
	TreeType       string `json:"tree_type"`
}

// ShopCatalog is GET /api/v1/mascot/shop.
func (h *MascotHandler) ShopCatalog(c *fiber.Ctx) error {
	if h == nil || h.shop == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "shop unavailable")
	}
	uid, ok := h.allow(c)
	if !ok {
		return nil
	}
	view, err := h.shop.Catalog(c.UserContext(), uid, time.Now().UTC())
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "shop_error", "could not load the shop")
	}
	return response.JSON(c, fiber.StatusOK, shopCatalogJSON(view))
}

// ShopBuy is POST /api/v1/mascot/shop/buy.
// A repeat of the same idempotency key returns the first receipt and does not charge again.
func (h *MascotHandler) ShopBuy(c *fiber.Ctx) error {
	if h == nil || h.shop == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "shop unavailable")
	}
	uid, ok := h.allow(c)
	if !ok {
		return nil
	}
	var body shopBuyBody
	if len(c.Body()) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	if err := decodeStrict(c.Body(), &body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	receipt, err := h.shop.Buy(c.UserContext(), uid, body.ItemID, body.TreeType, body.IdempotencyKey, time.Now().UTC())
	if err != nil {
		return writeShopErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, shopReceiptJSON(receipt))
}

func writeShopErr(c *fiber.Ctx, err error) error {
	var shopErr *repository.ShopError
	if errors.As(err, &shopErr) {
		status := fiber.StatusBadRequest
		switch shopErr.Code {
		case "already_owned", "streak_locked", "not_enough_coins":
			status = fiber.StatusConflict
		}
		body := fiber.Map{
			"code":    shopErr.Code,
			"message": usermsg.Plain(shopErr.Error()),
		}
		if shopErr.Code == "not_enough_coins" {
			body["needed"] = shopErr.Needed
			body["balance"] = shopErr.Balance
		}
		if shopErr.Code == "streak_locked" {
			body["streak"] = shopErr.Streak
			body["streak_required"] = shopErr.Need
		}
		if shopErr.Reason != "" {
			body["reason"] = shopErr.Reason
		}
		return c.Status(status).JSON(fiber.Map{"success": false, "error": body})
	}
	switch {
	case errors.Is(err, repository.ErrIdempotencyKey), errors.Is(err, repository.ErrTreeBadIdempotency):
		return response.Error(c, fiber.StatusBadRequest, "bad_idempotency_key", err.Error())
	case errors.Is(err, repository.ErrIdempotencyConflict):
		return response.Error(c, fiber.StatusConflict, "idempotency_conflict", err.Error())
	default:
		return response.Error(c, fiber.StatusInternalServerError, "shop_error", "could not buy that item")
	}
}

func shopCatalogJSON(view repository.ShopView) fiber.Map {
	owned := view.Owned
	if owned == nil {
		owned = map[string]struct{}{}
	}
	sets := make([]fiber.Map, 0, len(domain.ShopSets()))
	for _, set := range domain.ShopSets() {
		locked := set.StreakDays > 0 && !view.PearlUnlocked && view.Streak < set.StreakDays
		pieces := make([]fiber.Map, 0, len(set.Pieces))
		price := 0
		missing := 0
		for _, piece := range set.Pieces {
			price += piece.Price
			_, has := owned[piece.ID]
			if !has {
				missing += piece.Price
			}
			row := shopRow(piece.ID, piece.Name, piece.Layer, piece.ArtID, piece.Price, has, locked)
			if locked {
				row["lock_reason"] = "streak"
				row["streak"] = view.Streak
				row["streak_required"] = set.StreakDays
			}
			pieces = append(pieces, row)
		}
		_, bonusOwned := owned[set.Bonus.ID]
		charge := 0
		if !locked {
			charge = missing
		}
		purchasable := !locked && (missing > 0 || !bonusOwned)
		item := fiber.Map{
			"id":          set.ID,
			"name":        set.Name,
			"price":       price,
			"charge":      charge,
			"owned":       missing == 0,
			"locked":      locked,
			"purchasable": purchasable,
			"pieces":      pieces,
			"bonus":       shopRow(set.Bonus.ID, set.Bonus.Name, set.Bonus.Layer, set.Bonus.ArtID, 0, bonusOwned, false),
		}
		if set.StreakDays > 0 {
			item["streak"] = view.Streak
			item["streak_required"] = set.StreakDays
			if locked {
				item["lock_reason"] = "streak"
			}
		}
		sets = append(sets, item)
	}
	accessories := shopSKUGroup(domain.ShopAccessories(), owned)
	daily := shopSKUGroup(domain.ShopDaily(), owned)
	trees := make([]fiber.Map, 0, len(domain.ShopSeedTypes()))
	for _, row := range domain.ShopSeedTypes() {
		trees = append(trees, fiber.Map{
			"key":         row.Key,
			"name":        row.NameVI,
			"price":       domain.ShopSeedPrice,
			"purchasable": true,
		})
	}
	return fiber.Map{
		"balance":        view.Balance,
		"streak":         view.Streak,
		"pearl_unlocked": view.PearlUnlocked,
		"groups": []fiber.Map{
			{"id": domain.ShopGroupSets, "items": sets},
			{"id": domain.ShopGroupAccessories, "items": accessories},
			{
				"id":               domain.ShopGroupSeeds,
				"item_id":          domain.ShopSeedItemID,
				"price":            domain.ShopSeedPrice,
				"mystery_for_sale": false,
				"purchasable":      true,
				"tree_types":       trees,
			},
			{"id": domain.ShopGroupDaily, "items": daily},
		},
		"seasonal": fiber.Map{
			"purchasable": false,
			"reason":      "gift_box",
			"title":       "Đồ mùa và đồ hiếm chỉ có trong hộp quà cây",
		},
	}
}

func shopSKUGroup(rows []domain.ShopSKU, owned map[string]struct{}) []fiber.Map {
	out := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		_, has := owned[row.ID]
		out = append(out, shopRow(row.ID, row.Name, row.Layer, row.ArtID, row.Price, has, false))
	}
	return out
}

func shopRow(id, name, layer, art string, price int, owned, locked bool) fiber.Map {
	charge := 0
	if !owned && !locked && price > 0 {
		charge = price
	}
	row := fiber.Map{
		"id":          id,
		"name":        name,
		"layer":       layer,
		"price":       price,
		"charge":      charge,
		"owned":       owned,
		"locked":      locked,
		"purchasable": !owned && !locked && price > 0,
	}
	if art != "" {
		row["art_id"] = art
	}
	return row
}

func shopReceiptJSON(receipt repository.ShopReceipt) fiber.Map {
	return fiber.Map{
		"already":       receipt.Already,
		"item_id":       receipt.ItemID,
		"tree_type":     receipt.TreeType,
		"charged":       receipt.Charged,
		"balance":       receipt.Balance,
		"granted":       shopGrantJSON(receipt.Granted),
		"bonus_granted": shopGrantJSON(receipt.Bonus),
	}
}

func shopGrantJSON(pieces []domain.ShopPiece) []fiber.Map {
	out := make([]fiber.Map, 0, len(pieces))
	for _, piece := range pieces {
		out = append(out, fiber.Map{"id": piece.ID, "name": piece.Name, "layer": piece.Layer})
	}
	return out
}
