package handler

import (
	"bytes"
	"errors"
	"time"

	"github.com/dadiary/backend/internal/domain"
	airecovery "github.com/dadiary/backend/internal/usecase/airecovery"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// AdminAIRecoveryHandler replays skin-check analyses and onboarding starter
// routines that failed while model providers were out of credit.
type AdminAIRecoveryHandler struct {
	svc *airecovery.Service
}

// NewAdminAIRecoveryHandler constructs the handler.
func NewAdminAIRecoveryHandler(svc *airecovery.Service) *AdminAIRecoveryHandler {
	return &AdminAIRecoveryHandler{svc: svc}
}

type adminAIReplayRequest struct {
	Apply       bool   `json:"apply"`
	Since       string `json:"since"`
	Concurrency int    `json:"concurrency"`
	PauseMS     int    `json:"pause_ms"`
	StopAfter   int    `json:"stop_after"`
	Limit       int    `json:"limit"`
}

// Replay handles POST /api/v1/admin/ai/replay-outage.
//
// An empty body or `"apply": false` is a dry-run that only counts. `"apply": true`
// calls the providers, oldest first, and is safe to repeat: completed analyses
// and starter routines already marked as generated are skipped.
func (h *AdminAIRecoveryHandler) Replay(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "ai replay unavailable")
	}
	var body adminAIReplayRequest
	raw := bytes.TrimSpace(c.Body())
	if len(raw) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
		}
	}
	since := domain.AIAnalysisOutageSince
	if body.Since != "" {
		parsed, err := time.Parse(time.RFC3339, body.Since)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_input", "since must be RFC3339")
		}
		since = parsed
	}
	pause := time.Duration(body.PauseMS) * time.Millisecond
	if body.Apply && body.PauseMS == 0 {
		pause = airecovery.DefaultPause
	}
	res, err := h.svc.Run(c.UserContext(), airecovery.Options{
		Since:       since,
		Apply:       body.Apply,
		Concurrency: body.Concurrency,
		Pause:       pause,
		StopAfter:   body.StopAfter,
		Limit:       body.Limit,
	})
	if err != nil {
		if errors.Is(err, airecovery.ErrInvalidOptions) {
			return response.Error(c, fiber.StatusBadRequest, "invalid_input", "invalid replay options")
		}
		return response.Error(c, fiber.StatusInternalServerError, "ai_replay_failed", "could not replay failed ai work")
	}
	return response.JSON(c, fiber.StatusOK, res)
}
