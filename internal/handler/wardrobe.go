package handler

import (
	"errors"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	usageuc "github.com/dadiary/backend/internal/usecase/usage"
	wardrobeuc "github.com/dadiary/backend/internal/usecase/wardrobe"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// WardrobeHandler serves /wardrobe routes.
type WardrobeHandler struct {
	svc *wardrobeuc.Service
}

// NewWardrobeHandler constructs WardrobeHandler.
func NewWardrobeHandler(svc *wardrobeuc.Service) *WardrobeHandler {
	return &WardrobeHandler{svc: svc}
}

// CreateProduct handles POST /wardrobe/products.
func (h *WardrobeHandler) CreateProduct(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "wardrobe unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body dto.CreateWardrobeProductRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.Create(c.UserContext(), uid, body)
	if err != nil {
		return mapWardrobeWriteError(c, err)
	}
	return response.JSON(c, fiber.StatusCreated, res)
}

// UpdateProduct handles PATCH /wardrobe/products/:id.
func (h *WardrobeHandler) UpdateProduct(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "wardrobe unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	productID, err := parseWardrobeProductID(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "product id must be a UUID")
	}
	var body dto.UpdateWardrobeProductRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.Update(c.UserContext(), uid, productID, body)
	if err != nil {
		return mapWardrobeWriteError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// DeleteProduct handles DELETE /wardrobe/products/:id.
func (h *WardrobeHandler) DeleteProduct(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "wardrobe unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	productID, err := parseWardrobeProductID(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "product id must be a UUID")
	}
	if err := h.svc.Delete(c.UserContext(), uid, productID); err != nil {
		return mapWardrobeWriteError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

// List handles GET /wardrobe.
func (h *WardrobeHandler) List(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "wardrobe unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.svc.List(c.UserContext(), uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "wardrobe_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}

func parseWardrobeProductID(c *fiber.Ctx) (uuid.UUID, error) {
	return uuid.Parse(strings.TrimSpace(c.Params("id")))
}

func mapWardrobeWriteError(c *fiber.Ctx, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, wardrobeuc.ErrInvalidInput) {
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
	}
	if errors.Is(err, wardrobeuc.ErrNotFound) {
		return response.Error(c, fiber.StatusNotFound, "not_found", "product not found")
	}
	if errors.Is(err, usageuc.ErrPremiumRequired) || errors.Is(err, usageuc.ErrQuotaExceeded) {
		return mapPremiumGateError(c, domain.FeatureWardrobeFull, err)
	}
	return response.Error(c, fiber.StatusInternalServerError, "wardrobe_error", err.Error())
}
