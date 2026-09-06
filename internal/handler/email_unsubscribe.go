package handler

import (
	"strings"
	"time"

	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/email"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// EmailUnsubscribeHandler handles public one-click / link unsubscribe.
type EmailUnsubscribeHandler struct {
	signer *email.UnsubscribeSigner
	users  *repository.GormUserRepository
}

// NewEmailUnsubscribeHandler constructs the handler.
func NewEmailUnsubscribeHandler(
	signer *email.UnsubscribeSigner,
	users *repository.GormUserRepository,
) *EmailUnsubscribeHandler {
	return &EmailUnsubscribeHandler{signer: signer, users: users}
}

// Handle GET|POST /api/v1/email/unsubscribe
func (h *EmailUnsubscribeHandler) Handle(c *fiber.Ctx) error {
	if h == nil || h.signer == nil || h.users == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "unsubscribe unavailable")
	}
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		token = strings.TrimSpace(c.FormValue("token"))
	}
	uid, err := h.signer.Parse(token)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_token", "unsubscribe link is invalid")
	}
	if err := h.users.SetEmailUnsubscribedAt(c.UserContext(), uid, time.Now().UTC()); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "unsubscribe_failed", "could not unsubscribe")
	}

	if strings.Contains(c.Get("Accept"), "text/html") || c.Method() == fiber.MethodGet {
		c.Set("Content-Type", "text/html; charset=utf-8")
		return c.Status(fiber.StatusOK).SendString(`<!DOCTYPE html>
<html lang="vi"><head><meta charset="utf-8"><title>Đã hủy email DaDiary</title></head>
<body style="font-family:Georgia,serif;background:#faf6f2;color:#3d342e;padding:48px 20px;text-align:center;">
  <p>Bạn đã hủy nhận email nhắc từ DaDiary.</p>
  <p style="color:#8a7f78;font-size:14px;">Push và nhắc trong app không đổi. Có thể mở lại app bất cứ lúc nào.</p>
</body></html>`)
	}
	return response.JSONWithMessage(c, fiber.StatusOK, fiber.Map{"unsubscribed": true}, "unsubscribed")
}
