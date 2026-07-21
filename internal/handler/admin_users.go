package handler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	adminuseruc "github.com/dadiary/backend/internal/usecase/adminuser"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// AdminUsersHandler serves internal admin user search + plan grant/revoke.
type AdminUsersHandler struct {
	svc *adminuseruc.Service
}

// NewAdminUsersHandler constructs the handler.
func NewAdminUsersHandler(svc *adminuseruc.Service) *AdminUsersHandler {
	return &AdminUsersHandler{svc: svc}
}

// List handles GET /api/v1/admin/users?q=&page=&page_size=.
func (h *AdminUsersHandler) List(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin users unavailable")
	}
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		q = strings.TrimSpace(c.Query("query"))
	}
	page, pageSize := 1, 20
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			page = n
		}
	}
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			pageSize = n
		}
	}
	res, err := h.svc.Search(c.UserContext(), q, page, pageSize)
	if err != nil {
		if errors.Is(err, adminuseruc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "admin_users_error", "could not search users")
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// Get handles GET /api/v1/admin/users/:id.
func (h *AdminUsersHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin users unavailable")
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Params("id")))
	if err != nil || id == uuid.Nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "user id must be a valid UUID")
	}
	res, err := h.svc.Get(c.UserContext(), id)
	if err != nil {
		switch {
		case errors.Is(err, adminuseruc.ErrUnavailable):
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
		case errors.Is(err, adminuseruc.ErrNotFound):
			return response.Error(c, fiber.StatusNotFound, "not_found", "user not found")
		case errors.Is(err, adminuseruc.ErrInvalidInput):
			return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
		default:
			return response.Error(c, fiber.StatusInternalServerError, "admin_users_error", "could not load user")
		}
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// UpdatePlan handles PUT /api/v1/admin/users/:id/plan.
func (h *AdminUsersHandler) UpdatePlan(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin users unavailable")
	}
	actorID := middleware.UserIDFromLocals(c)
	if actorID == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	actorEmail, _ := c.Locals("auth_user_email").(string)

	targetID, err := uuid.Parse(strings.TrimSpace(c.Params("id")))
	if err != nil || targetID == uuid.Nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "user id must be a valid UUID")
	}

	var body dto.AdminUpdatePlanRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}

	res, err := h.svc.UpdatePlan(c.UserContext(), actorID, actorEmail, targetID, body)
	if err != nil {
		switch {
		case errors.Is(err, adminuseruc.ErrUnavailable):
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
		case errors.Is(err, adminuseruc.ErrNotFound):
			return response.Error(c, fiber.StatusNotFound, "not_found", "user not found")
		case errors.Is(err, adminuseruc.ErrSamePlan):
			return response.Error(c, fiber.StatusConflict, "plan_unchanged", "user already has this plan")
		case errors.Is(err, adminuseruc.ErrInvalidInput):
			return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
		default:
			return response.Error(c, fiber.StatusInternalServerError, "admin_plan_error", "could not update plan")
		}
	}
	return response.JSON(c, fiber.StatusOK, res)
}
