package handler

import (
	"errors"
	"log/slog"

	resendhookuc "github.com/dadiary/backend/internal/usecase/resendhook"
	"github.com/gofiber/fiber/v2"
)

// ResendWebhookHandler serves POST /api/v1/email/resend/webhook.
// Auth is the Svix signature only (no JWT).
type ResendWebhookHandler struct {
	svc *resendhookuc.Service
}

// NewResendWebhookHandler constructs the handler.
func NewResendWebhookHandler(svc *resendhookuc.Service) *ResendWebhookHandler {
	return &ResendWebhookHandler{svc: svc}
}

// Handle accepts Resend email.opened and email.clicked events.
// Any other event type is a 200 no-op. Duplicate Svix ids are 200 with no second row.
func (h *ResendWebhookHandler) Handle(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"success": false,
			"message": "resend webhook unavailable",
		})
	}
	id := firstHeader(c, "svix-id", "webhook-id")
	ts := firstHeader(c, "svix-timestamp", "webhook-timestamp")
	sig := firstHeader(c, "svix-signature", "webhook-signature")
	body := c.Body()

	out, err := h.svc.Handle(c.UserContext(), id, ts, sig, body)
	if err != nil {
		switch {
		case errors.Is(err, resendhookuc.ErrNotConfigured):
			slog.Error("resend_webhook: secret missing")
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"success": false,
				"code":    "webhook_not_configured",
			})
		case errors.Is(err, resendhookuc.ErrInvalidSignature):
			slog.Warn("resend_webhook: signature rejected", "body_bytes", len(body))
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"code":    "invalid_signature",
			})
		case errors.Is(err, resendhookuc.ErrInvalidPayload):
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false,
				"code":    "invalid_payload",
			})
		default:
			slog.Error("resend_webhook: store failed", "err", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"success": false,
				"code":    "webhook_error",
			})
		}
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success":    true,
		"stored":     out.Stored,
		"duplicate":  out.Duplicate,
		"ignored":    out.Ignored,
		"event_type": out.EventType,
	})
}

func firstHeader(c *fiber.Ctx, names ...string) string {
	for _, name := range names {
		if v := c.Get(name); v != "" {
			return v
		}
	}
	return ""
}
