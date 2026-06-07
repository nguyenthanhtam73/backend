package handler

import (
	"github.com/dadiary/backend/internal/middleware"
	usageuc "github.com/dadiary/backend/internal/usecase/usage"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// MeUsageHandler exposes free-plan quota counters for the client UI.
type MeUsageHandler struct {
	usage *usageuc.Service
}

// NewMeUsageHandler constructs MeUsageHandler.
func NewMeUsageHandler(usage *usageuc.Service) *MeUsageHandler {
	return &MeUsageHandler{usage: usage}
}

// Get handles GET /me/usage.
func (h *MeUsageHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.usage == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "usage unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.usage.GetQuota(c.UserContext(), uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "usage_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}
