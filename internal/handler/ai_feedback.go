package handler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	aifeedbackuc "github.com/dadiary/backend/internal/usecase/aifeedback"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// AIFeedbackHandler handles POST /ai/feedback and GET /ai/feedback/me.
type AIFeedbackHandler struct {
	svc *aifeedbackuc.Service
}

// NewAIFeedbackHandler constructs handler.
func NewAIFeedbackHandler(svc *aifeedbackuc.Service) *AIFeedbackHandler {
	return &AIFeedbackHandler{svc: svc}
}

// Create handles POST /api/v1/ai/feedback.
func (h *AIFeedbackHandler) Create(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "feedback unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body dto.CreateAIFeedbackRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.Create(c.UserContext(), uid, body)
	if err != nil {
		if errors.Is(err, aifeedbackuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "feedback_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusBadRequest, "invalid_feedback", err.Error())
	}
	return response.JSON(c, fiber.StatusCreated, res)
}

// List handles GET /api/v1/ai/feedback/me?limit=50.
//
// Returns the authenticated user's most recent feedback rows (newest first)
// so the UI can render a personal feedback history view later. The endpoint
// is also useful for debugging the prompt loop — POSTing votes and reading
// them back confirms the round-trip.
func (h *AIFeedbackHandler) List(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "feedback unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	res, err := h.svc.ListMine(c.UserContext(), uid, limit)
	if err != nil {
		if errors.Is(err, aifeedbackuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "feedback_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "feedback_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}
