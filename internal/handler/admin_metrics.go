package handler

import (
	"errors"
	"strconv"
	"strings"

	adminmetricsuc "github.com/dadiary/backend/internal/usecase/adminmetrics"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// AdminMetricsHandler exposes admin-only monitoring dashboards.
type AdminMetricsHandler struct {
	svc *adminmetricsuc.Service
}

// NewAdminMetricsHandler constructs the handler.
func NewAdminMetricsHandler(svc *adminmetricsuc.Service) *AdminMetricsHandler {
	return &AdminMetricsHandler{svc: svc}
}

// Payment handles GET /api/v1/admin/metrics/payment
// Query: status (optional), limit (default 50), offset (default 0).
func (h *AdminMetricsHandler) Payment(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin metrics unavailable")
	}

	q := adminmetricsuc.PaymentMetricsQuery{
		Status: strings.TrimSpace(c.Query("status")),
		Limit:  50,
		Offset: 0,
	}
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			q.Limit = n
		}
	}
	if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			q.Offset = n
		}
	}

	out, err := h.svc.PaymentMetrics(c.UserContext(), q)
	if err != nil {
		if errors.Is(err, adminmetricsuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "metrics_error", "could not load payment metrics")
	}
	return response.JSON(c, fiber.StatusOK, out)
}
