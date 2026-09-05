package handler

import (
	subscriptionuc "github.com/dadiary/backend/internal/usecase/subscription"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// AdminBillingHandler exposes operator billing repair.
type AdminBillingHandler struct {
	svc *subscriptionuc.Service
}

// NewAdminBillingHandler constructs the handler.
func NewAdminBillingHandler(svc *subscriptionuc.Service) *AdminBillingHandler {
	return &AdminBillingHandler{svc: svc}
}

// Reconcile handles POST /api/v1/admin/billing/reconcile.
// Idempotent: expires overdue subscription rows and syncs users.
func (h *AdminBillingHandler) Reconcile(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "subscription unavailable")
	}
	res, err := h.svc.ReconcileBillingState(c.UserContext())
	if err != nil {
		status, code, msg := subscriptionuc.MapError(err)
		return response.Error(c, status, code, msg)
	}
	return response.JSONWithMessage(c, fiber.StatusOK, res, "billing_reconciled")
}
