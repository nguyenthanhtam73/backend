// Package handler — routine.go exposes the Routine Management endpoints:
//
//	GET    /api/v1/routines              → today's routine (or carry-forward)
//	POST   /api/v1/routines              → create / update today's routine
//	GET    /api/v1/routines/history      → past N days for the progress view
//	POST   /api/v1/routines/suggest      → AI-generated routine (no persist)
package handler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	routineuc "github.com/dadiary/backend/internal/usecase/routine"
	usageuc "github.com/dadiary/backend/internal/usecase/usage"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// RoutineHandler serves /routines routes.
type RoutineHandler struct {
	svc     *routineuc.Service
	premium *premiumuc.Service
}

// NewRoutineHandler constructs the handler. premium may be nil (no_ads strip skipped).
func NewRoutineHandler(svc *routineuc.Service, premium *premiumuc.Service) *RoutineHandler {
	return &RoutineHandler{svc: svc, premium: premium}
}

// GetCurrent handles GET /api/v1/routines.
//
// Returns today's saved row (Saved=true). When today has no row yet, falls
// back to the latest entry "carried over" so the UI always has something to
// render. If no row has ever been saved, returns an empty projection (the
// frontend renders the empty-state CTA in that case).
func (h *RoutineHandler) GetCurrent(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "routine service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.svc.GetCurrent(c.UserContext(), uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "routine_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// Put handles POST /api/v1/routines.
//
// We use POST (not PUT) to keep the route shape symmetric with the rest of the
// DaDiary API (/skin-checks, /wardrobe/products) and because some mobile
// clients don't love PUT on cellular networks (proxies sometimes drop it).
// Upsert semantics: server creates the row if absent, updates it if present.
func (h *RoutineHandler) Put(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "routine service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body dto.PutRoutineRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.Upsert(c.UserContext(), uid, body)
	if err != nil {
		return mapRoutineError(c, domain.FeatureEditRoutine, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// History handles GET /api/v1/routines/history?range=30.
//
// `range` is the lookback in days (1..365). Returns each saved entry in the
// window plus a streak count + average completion ratio. Frontend uses this
// for the "Tiến trình routine" panel.
func (h *RoutineHandler) History(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "routine service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	rangeDays := parseRoutineRange(c.Query("range"))
	res, err := h.svc.History(c.UserContext(), uid, rangeDays)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "routine_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// Suggest handles POST /api/v1/routines/suggest.
//
// Enqueues an async AI job and returns { job_id, status: "processing" }.
// Poll GET /routines/suggest/status?job_id=... for the result.
func (h *RoutineHandler) Suggest(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "routine service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body dto.SuggestRoutineRequest
	if c.Get("Content-Type") != "" && len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
		}
	}
	res, err := h.svc.StartSuggestJob(c.UserContext(), uid, body)
	if err != nil {
		return mapRoutineError(c, domain.FeatureAIRoutineSuggestion, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// SuggestStatus handles GET /api/v1/routines/suggest/status?job_id=...
func (h *RoutineHandler) SuggestStatus(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "routine service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	jobID := strings.TrimSpace(c.Query("job_id"))
	if jobID == "" {
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", "job_id is required")
	}
	res, ok, err := h.svc.GetSuggestJobStatus(uid, jobID)
	if err != nil {
		return mapRoutineError(c, domain.FeatureAIRoutineSuggestion, err)
	}
	if !ok {
		return response.Error(c, fiber.StatusNotFound, "not_found", "suggest job not found or expired")
	}
	stripSuggestAds(c.UserContext(), h.premium, uid, &res)
	return response.JSON(c, fiber.StatusOK, res)
}

// CancelSuggest handles DELETE /api/v1/routines/suggest?job_id=...
func (h *RoutineHandler) CancelSuggest(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "routine service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	jobID := strings.TrimSpace(c.Query("job_id"))
	if jobID == "" {
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", "job_id is required")
	}
	if !h.svc.CancelSuggestJob(uid, jobID) {
		return response.Error(c, fiber.StatusNotFound, "not_found", "suggest job not found or expired")
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"job_id": jobID,
		"status": "cancelled",
	})
}

func mapRoutineError(c *fiber.Ctx, feature domain.Feature, err error) error {
	switch {
	case errors.Is(err, routineuc.ErrInvalidInput):
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, routineuc.ErrUnavailable):
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
	case errors.Is(err, usageuc.ErrPremiumRequired),
		errors.Is(err, usageuc.ErrQuotaExceeded),
		errors.Is(err, premiumuc.ErrFeatureDenied),
		errors.Is(err, premiumuc.ErrQuotaExceeded):
		return mapPremiumGateError(c, feature, err)
	default:
		return response.Error(c, fiber.StatusInternalServerError, "routine_error", err.Error())
	}
}

func parseRoutineRange(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 30
	}
	if raw == "all" {
		return 365
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 30
	}
	if n > 365 {
		n = 365
	}
	return n
}
