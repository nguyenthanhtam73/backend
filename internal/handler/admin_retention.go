package handler

import (
	"errors"

	"github.com/dadiary/backend/internal/config"
	adminretentionuc "github.com/dadiary/backend/internal/usecase/adminretention"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// AdminRetentionHandler serves GET /api/v1/admin/retention-stats.
// The handler is read-only: the service issues SELECT queries only.
type AdminRetentionHandler struct {
	svc *adminretentionuc.Service
	cfg *config.Config
}

// NewAdminRetentionHandler constructs the handler.
func NewAdminRetentionHandler(svc *adminretentionuc.Service, cfg *config.Config) *AdminRetentionHandler {
	return &AdminRetentionHandler{svc: svc, cfg: cfg}
}

// Get handles GET /api/v1/admin/retention-stats (admin JWT).
//
// Query: from, to — optional YYYY-MM-DD Vietnam civil dates, inclusive,
// filtering by registration day. Omitted means all time.
func (h *AdminRetentionHandler) Get(c *fiber.Ctx) error {
	from, to, err := adminretentionuc.ParseDayRange(c.Query("from"), c.Query("to"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_date", err.Error())
	}
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin retention stats unavailable")
	}
	out, err := h.svc.Stats(c.UserContext(), adminretentionuc.Query{
		From:           from,
		To:             to,
		ExcludedEmails: config.RetentionStatsExcludedEmails(h.cfg),
	})
	if err != nil {
		var dateErr *adminretentionuc.DateError
		if errors.As(err, &dateErr) {
			return response.Error(c, fiber.StatusBadRequest, "invalid_date", dateErr.Error())
		}
		if errors.Is(err, adminretentionuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "retention_stats_error", "could not load retention stats")
	}
	return response.JSON(c, fiber.StatusOK, out)
}
