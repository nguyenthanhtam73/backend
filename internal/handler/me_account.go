package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/dadiary/backend/internal/middleware"
	userdatauc "github.com/dadiary/backend/internal/usecase/userdata"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// DeleteAccount handles DELETE /api/v1/me.
// Body is {"password":"..."}. Success is 204 with an empty body.
// A wrong or missing password is 401 invalid_password.
func (h *MeDataHandler) DeleteAccount(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "user data service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	password, err := deleteAccountPassword(c)
	if err != nil {
		return err
	}
	if err := h.svc.DeleteAccount(c.UserContext(), uid, password); err != nil {
		if errors.Is(err, userdatauc.ErrSchemaNotReady) {
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "account deletion is temporarily unavailable")
		}
		if errors.Is(err, userdatauc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
		}
		if errors.Is(err, userdatauc.ErrInvalidUser) {
			return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
		}
		if errors.Is(err, userdatauc.ErrInvalidPassword) {
			return response.Error(c, fiber.StatusUnauthorized, "invalid_password", "invalid password")
		}
		return response.Error(c, fiber.StatusInternalServerError, "delete_failed", "could not delete account")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func deleteAccountPassword(c *fiber.Ctx) (string, error) {
	raw := bytes.TrimSpace(c.Body())
	if len(raw) == 0 {
		return "", response.Error(c, fiber.StatusUnauthorized, "invalid_password", "invalid password")
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", response.Error(c, fiber.StatusBadRequest, "invalid_json", "request body must be valid JSON")
	}
	password := strings.TrimSpace(body.Password)
	if password == "" {
		return "", response.Error(c, fiber.StatusUnauthorized, "invalid_password", "invalid password")
	}
	return password, nil
}
