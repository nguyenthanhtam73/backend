package handler

import (
	"log/slog"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	paymentuc "github.com/dadiary/backend/internal/usecase/payment"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// PaymentSePayHandler exposes SePay checkout + IPN webhook.
type PaymentSePayHandler struct {
	svc *paymentuc.Service
}

// NewPaymentSePayHandler constructs the handler.
func NewPaymentSePayHandler(svc *paymentuc.Service) *PaymentSePayHandler {
	return &PaymentSePayHandler{svc: svc}
}

// CreateCheckout handles POST /api/v1/payment/sepay/checkout (JWT).
// Persists payment_orders, returns checkout_url + signed form_fields for the FE.
func (h *PaymentSePayHandler) CreateCheckout(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "payment unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	var req dto.CreateSePayCheckoutRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "invalid JSON body")
	}

	out, err := h.svc.CreatePayment(c.UserContext(), uid, req)
	if err != nil {
		status, code, msg := paymentuc.MapError(err)
		return response.Error(c, status, code, msg)
	}
	return response.JSON(c, fiber.StatusOK, out)
}

// Webhook handles POST /api/v1/payment/sepay/webhook (public — SePay IPN).
//
// Auth: header X-Secret-Key must match DADIARY_SEPAY_SECRET_KEY.
// On ORDER_PAID → mark payment_orders paid + upgrade users.plan_tier.
// Response MUST be HTTP 200 with {"success": true} so SePay stops retrying.
func (h *PaymentSePayHandler) Webhook(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		// Still ack 200 when misconfigured? Prefer 503 so SePay retries after deploy.
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"success": false,
			"message": "payment unavailable",
		})
	}

	secret := c.Get("X-Secret-Key")
	raw := c.Body() // Fiber keeps raw body — required for future HMAC verification

	if err := h.svc.HandleSePayWebhook(c.UserContext(), secret, raw); err != nil {
		status, code, msg := paymentuc.MapError(err)
		slog.Warn("payment: sepay webhook rejected",
			"status", status,
			"code", code,
			"message", msg,
		)
		// 401/409 → tell SePay not to treat as success; 404 may be probe — still fail closed.
		return c.Status(status).JSON(fiber.Map{
			"success": false,
			"message": msg,
			"code":    code,
		})
	}

	// SePay IPN contract: plain {"success": true} (not our data envelope).
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"success": true})
}
