package handler

import (
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	dashboarduc "github.com/dadiary/backend/internal/usecase/dashboard"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// DashboardHandler serves GET /api/v1/me/dashboard (home summary aggregate).
type DashboardHandler struct {
	svc *dashboarduc.Usecase
}

// NewDashboardHandler constructs the handler.
func NewDashboardHandler(svc *dashboarduc.Usecase) *DashboardHandler {
	return &DashboardHandler{svc: svc}
}

// GetSummary handles GET /me/dashboard.
func (h *DashboardHandler) GetSummary(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "dashboard unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.svc.GetSummary(c.UserContext(), uid)
	if err != nil {
		if ae, ok := domain.AsAppError(err); ok {
			return response.Error(c, ae.HTTPStatus, ae.Code, ae.Message)
		}
		return response.Error(c, fiber.StatusInternalServerError, "dashboard_error", "could not load dashboard")
	}
	return response.JSONWithMessage(c, fiber.StatusOK, res, "ok")
}
