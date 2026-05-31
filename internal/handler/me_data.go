package handler

import (
	"errors"

	"github.com/dadiary/backend/internal/middleware"
	userdatauc "github.com/dadiary/backend/internal/usecase/userdata"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// MeDataHandler serves DELETE /api/v1/me/data.
type MeDataHandler struct {
	svc *userdatauc.Service
}

// NewMeDataHandler constructs the handler.
func NewMeDataHandler(svc *userdatauc.Service) *MeDataHandler {
	return &MeDataHandler{svc: svc}
}

// Delete handles DELETE /me/data — soft-deletes all personal diary rows.
func (h *MeDataHandler) Delete(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "user data service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.svc.DeleteAll(c.UserContext(), uid)
	if err != nil {
		if errors.Is(err, userdatauc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
		}
		if errors.Is(err, userdatauc.ErrInvalidUser) {
			return response.Error(c, fiber.StatusBadRequest, "invalid_user", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "delete_failed", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}
