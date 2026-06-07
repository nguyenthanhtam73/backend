package handler

import (
	"errors"

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
		if errors.Is(err, wardrobeuc.ErrInvalidInput) {
			return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
		}
		if errors.Is(err, usageuc.ErrPremiumRequired) {
			return response.Error(c, fiber.StatusForbidden, "premium_required", "upgrade to Premium to manage your skincare cabinet")
		}
		return response.Error(c, fiber.StatusInternalServerError, "wardrobe_error", err.Error())
	}
	return response.JSON(c, fiber.StatusCreated, res)
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
