// Package handler — progress.go exposes GET /api/v1/progress and
// GET /api/v1/progress/summary for the Timeline + Before-After page.
package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/streaktime"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// ProgressHandler serves the Progress Timeline + Summary endpoints.
//
// We deliberately keep this lean (no extra service layer) because both endpoints
// are pure read aggregations on top of `skin_checks` + `skin_analyses`. There is
// no LLM call here — all motivational numbers are computed deterministically from
// stored gauges, which keeps the page fast and free to render.
type ProgressHandler struct {
	repo  *repository.GormSkinCheckRepository
	gates *premiumuc.Service
}

// NewProgressHandler constructs the handler.
// gates may be nil (no plan clamp — treated as Free 3-month window when present checks fail open to parse only).
func NewProgressHandler(repo *repository.GormSkinCheckRepository, gates *premiumuc.Service) *ProgressHandler {
	return &ProgressHandler{repo: repo, gates: gates}
}

// Timeline handles GET /api/v1/progress?range=30|90|180|all.
//
// Query parameters:
//   - range (optional): one of "30", "90", "180", "365", or "all". Default = 30.
//   - limit (optional, dev-only escape hatch): caps the entries returned.
//
// Response: dto.ProgressTimelineResponse with `entries` (newest first) and an
// inline `summary` block — one round-trip is enough to render the whole page.
func (h *ProgressHandler) Timeline(c *fiber.Ctx) error {
	if h == nil || h.repo == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "progress service is not available")
	}
	userID := middleware.UserIDFromLocals(c)
	if userID == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	rangeDays, since := h.clampProgressRange(c, userID, c.Query("range"))
	limit := 0
	if lq := strings.TrimSpace(c.Query("limit")); lq != "" {
		if n, err := strconv.Atoi(lq); err == nil && n > 0 {
			limit = n
		}
	}

	rows, err := h.repo.ListForOwner(c.UserContext(), userID, since, limit)
	if err != nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "database_error", err.Error())
	}
	out := dto.NewProgressTimelineResponse(rows, rangeDays, "/uploads")
	return response.JSON(c, fiber.StatusOK, out)
}

// Summary handles GET /api/v1/progress/summary?range=...
//
// Same query params as Timeline, but only the `summary` block is returned. Useful
// for the home/dashboard hero where the full entry list would be wasted bytes.
func (h *ProgressHandler) Summary(c *fiber.Ctx) error {
	if h == nil || h.repo == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "progress service is not available")
	}
	userID := middleware.UserIDFromLocals(c)
	if userID == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	rangeDays, since := h.clampProgressRange(c, userID, c.Query("range"))
	rows, err := h.repo.ListForOwner(c.UserContext(), userID, since, 0)
	if err != nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "database_error", err.Error())
	}
	full := dto.NewProgressTimelineResponse(rows, rangeDays, "")
	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"range_days": full.RangeDays,
		"from":       full.From,
		"to":         full.To,
		"summary":    full.Summary,
	})
}

// clampProgressRange applies plan history windows (Free 3mo / Premium 12mo / Plus all).
func (h *ProgressHandler) clampProgressRange(c *fiber.Ctx, userID uuid.UUID, raw string) (int, time.Time) {
	rangeDays, since := parseProgressRange(raw)
	if h == nil || h.gates == nil {
		return rangeDays, since
	}
	months, err := h.gates.ProgressHistoryMonths(c.UserContext(), userID)
	if err != nil {
		// Fail closed to Free window when plan lookup fails.
		months = 3
	}
	return premiumuc.ClampProgressRange(months, rangeDays, streaktime.Today())
}

// parseProgressRange normalizes the `range` query parameter.
//
// Returned values:
//   - rangeDays: 0 for "all"; otherwise the lookback window in days.
//   - since:     time.Time{} for "all"; otherwise the lower bound for `check_date >= since`.
//
// Unknown values fall back to 30 days (the safest "what did this week look like" default).
func parseProgressRange(raw string) (int, time.Time) {
	today := streaktime.Today()
	r := strings.ToLower(strings.TrimSpace(raw))
	switch r {
	case "all", "0":
		return 0, time.Time{}
	case "90", "3m":
		return 90, today.AddDate(0, 0, -90)
	case "180", "6m":
		return 180, today.AddDate(0, 0, -180)
	case "365", "1y", "12m":
		return 365, today.AddDate(0, 0, -365)
	case "", "30", "1m":
		return 30, today.AddDate(0, 0, -30)
	}
	// Numeric custom range (any positive integer of days, capped at 730).
	if n, err := strconv.Atoi(r); err == nil && n > 0 {
		if n > 730 {
			n = 730
		}
		return n, today.AddDate(0, 0, -n)
	}
	return 30, today.AddDate(0, 0, -30)
}
