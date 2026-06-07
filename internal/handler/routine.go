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

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	usageuc "github.com/dadiary/backend/internal/usecase/usage"
	routineuc "github.com/dadiary/backend/internal/usecase/routine"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// RoutineHandler serves /routines routes.
type RoutineHandler struct {
	svc *routineuc.Service
}

// NewRoutineHandler constructs the handler.
func NewRoutineHandler(svc *routineuc.Service) *RoutineHandler {
	return &RoutineHandler{svc: svc}
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
		return mapRoutineError(c, err)
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
// Calls the AI to generate a fresh AM/PM routine for the user. We deliberately
// keep this read-only (no DB write) so the user can preview, tweak, and then
// explicitly save with POST /routines. Lots of users will reject the first
// suggestion — persisting on every call would pollute history.
func (h *RoutineHandler) Suggest(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "routine service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body dto.SuggestRoutineRequest
	// Body is optional — empty POST is a valid "use defaults from profile" call.
	if c.Get("Content-Type") != "" && len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
		}
	}
	res, err := h.svc.Suggest(c.UserContext(), uid, body)
	if err != nil {
		return mapRoutineError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

func mapRoutineError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, routineuc.ErrInvalidInput):
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, routineuc.ErrUnavailable):
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
	case errors.Is(err, usageuc.ErrPremiumRequired):
		return response.Error(c, fiber.StatusForbidden, "premium_required", "upgrade to Premium to use this feature")
	case errors.Is(err, usageuc.ErrQuotaExceeded):
		return response.Error(c, fiber.StatusForbidden, "quota_exceeded", "monthly free limit reached — upgrade to Premium for unlimited access")
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
