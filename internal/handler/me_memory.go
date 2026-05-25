package handler

import (
	"errors"
	"strings"

	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/usecase/usermemory"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// MeMemoryHandler serves GET /api/v1/me/memory — a read-only debug/inspect
// endpoint that returns the same long-term memory block we inject into AI
// coach prompts (plus a small diagnostic block).
//
// Why we expose this:
//   - The frontend can show users "what does the AI know about me?" which
//     builds trust and helps surface bad data quickly.
//   - Backend devs can verify the prompt loop end-to-end without touching
//     the DB. `?fresh=1` forces a cache-miss rebuild for ground truth.
type MeMemoryHandler struct {
	svc *usermemory.Service
}

// NewMeMemoryHandler constructs the handler.
func NewMeMemoryHandler(svc *usermemory.Service) *MeMemoryHandler {
	return &MeMemoryHandler{svc: svc}
}

// Get handles GET /api/v1/me/memory[?fresh=1].
//
// Query parameters:
//   - fresh=1 (or "true") bypasses the 5-minute TTL cache and forces a fresh
//     rebuild. The `cached` field in the response will be `false`.
func (h *MeMemoryHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "memory service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	forceFresh := parseBoolQuery(c.Query("fresh"))

	res, err := h.svc.Get(c.UserContext(), uid, forceFresh)
	if err != nil {
		if errors.Is(err, usermemory.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "memory_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "memory_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// parseBoolQuery accepts "1", "true", "yes", "on" (case-insensitive) as true.
// Anything else (including empty) is false. Keeps the API forgiving — both
// `?fresh=1` and `?fresh=true` work.
func parseBoolQuery(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
