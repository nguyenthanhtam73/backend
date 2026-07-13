package handler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	feedbackuc "github.com/dadiary/backend/internal/usecase/feedback"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// FeedbackHandler serves user feedback submission and admin triage endpoints.
type FeedbackHandler struct {
	svc *feedbackuc.Service
}

// NewFeedbackHandler constructs handler.
func NewFeedbackHandler(svc *feedbackuc.Service) *FeedbackHandler {
	return &FeedbackHandler{svc: svc}
}

// Create handles POST /api/v1/feedbacks.
func (h *FeedbackHandler) Create(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "feedback unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body dto.CreateFeedbackRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.Create(c.UserContext(), uid, body)
	if err != nil {
		if errors.Is(err, feedbackuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "feedback_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusBadRequest, "invalid_feedback", err.Error())
	}
	return response.JSON(c, fiber.StatusCreated, res)
}

// AdminList handles GET /api/v1/admin/feedbacks.
func (h *FeedbackHandler) AdminList(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "feedback unavailable")
	}
	filter := repository.FeedbackListFilter{
		Type:   strings.TrimSpace(c.Query("type")),
		Status: strings.TrimSpace(c.Query("status")),
	}
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			filter.Page = n
		}
	}
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			filter.PageSize = n
		}
	}
	res, err := h.svc.ListAdmin(c.UserContext(), filter)
	if err != nil {
		if errors.Is(err, feedbackuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "feedback_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "feedback_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// AdminUpdateStatus handles PATCH /api/v1/admin/feedbacks/:id.
func (h *FeedbackHandler) AdminUpdateStatus(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "feedback unavailable")
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Params("id")))
	if err != nil || id == uuid.Nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "id must be a valid UUID")
	}
	var body dto.UpdateFeedbackStatusRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.UpdateStatus(c.UserContext(), id, strings.TrimSpace(body.Status))
	if err != nil {
		if errors.Is(err, feedbackuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "feedback_unavailable", err.Error())
		}
		if errors.Is(err, feedbackuc.ErrNotFound) {
			return response.Error(c, fiber.StatusNotFound, "not_found", "feedback not found")
		}
		return response.Error(c, fiber.StatusBadRequest, "invalid_feedback", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}
