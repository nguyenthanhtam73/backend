package handler

import (
	"bytes"
	"time"

	"github.com/dadiary/backend/internal/usecase/outageapology"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// AdminOutageApologyHandler sends the November 2026 apology email.
// Same DADIARY_ADMIN_EMAILS gate as other /admin routes.
type AdminOutageApologyHandler struct {
	svc *outageapology.Service
}

// NewAdminOutageApologyHandler constructs the handler.
func NewAdminOutageApologyHandler(svc *outageapology.Service) *AdminOutageApologyHandler {
	return &AdminOutageApologyHandler{svc: svc}
}

type adminOutageApologyRequest struct {
	Apply     bool `json:"apply"`
	Test      bool `json:"test"`
	PauseMS   int  `json:"pause_ms"`
	StopAfter int  `json:"stop_after"`
}

// Send handles POST /api/v1/admin/ai/outage-apology.
// An empty body is a dry-run (recipient count only). "test": true sends one
// has-result email to the first admin address. "apply": true sends to users.
func (h *AdminOutageApologyHandler) Send(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "outage apology unavailable")
	}
	var body adminOutageApologyRequest
	raw := bytes.TrimSpace(c.Body())
	if len(raw) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
		}
	}
	if body.Apply && body.Test {
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", "pass only one of apply or test")
	}
	mode := outageapology.ModeDryRun
	switch {
	case body.Test:
		mode = outageapology.ModeTest
	case body.Apply:
		mode = outageapology.ModeApply
	}
	pause := time.Duration(body.PauseMS) * time.Millisecond
	if body.Apply && body.PauseMS == 0 {
		pause = outageapology.DefaultPause
	}
	res, err := h.svc.Run(c.UserContext(), mode, pause, body.StopAfter)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "outage_apology_failed", "could not send outage apology")
	}
	return response.JSON(c, fiber.StatusOK, res)
}
