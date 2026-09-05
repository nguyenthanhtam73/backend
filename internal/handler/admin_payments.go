package handler

import (
	"time"

	paymentuc "github.com/dadiary/backend/internal/usecase/payment"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// AdminPaymentsHandler exposes operator payment-order hygiene.
type AdminPaymentsHandler struct {
	svc *paymentuc.Service
}

// NewAdminPaymentsHandler constructs the handler.
func NewAdminPaymentsHandler(svc *paymentuc.Service) *AdminPaymentsHandler {
	return &AdminPaymentsHandler{svc: svc}
}

// ExpirePending handles POST /api/v1/admin/payments/expire-pending.
// Idempotent: only leftover status=pending rows older than the TTL are expired.
func (h *AdminPaymentsHandler) ExpirePending(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "payment unavailable")
	}
	res, err := h.svc.ExpireStalePending(c.UserContext(), time.Time{})
	if err != nil {
		status, code, msg := paymentuc.MapError(err)
		return response.Error(c, status, code, msg)
	}
	return response.JSONWithMessage(c, fiber.StatusOK, res.ToDTO(), "pending_orders_expired")
}
