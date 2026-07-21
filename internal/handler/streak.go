package handler

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	streakuc "github.com/dadiary/backend/internal/usecase/streak"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// StreakHandler serves the authenticated user's streak summary and admin repair.
type StreakHandler struct {
	svc     *streakuc.Service
	premium *premiumuc.Service
}

// NewStreakHandler constructs handler. premium may be nil (milestones fall back to Free).
func NewStreakHandler(svc *streakuc.Service, premium *premiumuc.Service) *StreakHandler {
	return &StreakHandler{svc: svc, premium: premium}
}

// Get handles GET /api/v1/me/streak.
func (h *StreakHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "streak unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.svc.Get(c.UserContext(), uid)
	if err != nil {
		if errors.Is(err, streakuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "streak_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "streak_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// Milestones handles GET /api/v1/me/streak/milestones.
//
// Free users always receive the basic catalog (3 + 7) without error.
// Pass ?full=1 to request the Premium catalog — AssertFeature(milestone_full)
// returns 403 + reason when the plan does not include it.
func (h *StreakHandler) Milestones(c *fiber.Ctx) error {
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	wantFull := strings.EqualFold(strings.TrimSpace(c.Query("full")), "1") ||
		strings.EqualFold(strings.TrimSpace(c.Query("full")), "true")

	tier := domain.PlanFree
	fullAccess := false
	if h.premium != nil {
		t, err := h.premium.PlanTier(c.UserContext(), uid)
		if err == nil {
			tier = t
		}
		ok, _, err := h.premium.CanUseFeature(c.UserContext(), uid, domain.FeatureMilestoneFull)
		if err == nil {
			fullAccess = ok
		}
		if wantFull {
			if err := h.premium.AssertFeature(c.UserContext(), uid, domain.FeatureMilestoneFull); err != nil {
				return mapPremiumGateError(c, domain.FeatureMilestoneFull, err)
			}
			fullAccess = true
		}
	} else if wantFull {
		return mapPremiumGateError(c, domain.FeatureMilestoneFull, premiumuc.ErrUnavailable)
	}

	var defs []domain.MilestoneDef
	if fullAccess {
		defs = domain.MilestoneCatalogForPlan(domain.PlanPremium)
	} else {
		defs = domain.MilestoneCatalogForPlan(domain.PlanFree)
	}

	days := make([]int, len(defs))
	items := make([]dto.MilestoneItemDTO, len(defs))
	for i, d := range defs {
		days[i] = d.Days
		items[i] = dto.MilestoneItemDTO{
			Days:    d.Days,
			Tier:    d.Tier,
			CopyKey: d.CopyKey,
		}
	}
	return response.JSON(c, fiber.StatusOK, dto.MilestoneCatalogResponse{
		PlanTier:      string(domain.NormalizePlanTier(tier)),
		FullAccess:    fullAccess,
		MilestoneDays: days,
		Items:         items,
	})
}

// UseFreeze handles POST /api/v1/me/streak/freeze.
func (h *StreakHandler) UseFreeze(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "streak unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.svc.UseFreeze(c.UserContext(), uid)
	if err != nil {
		switch {
		case errors.Is(err, streakuc.ErrUnavailable):
			return response.Error(c, fiber.StatusServiceUnavailable, "streak_unavailable", err.Error())
		case errors.Is(err, streakuc.ErrNoFreezes):
			return response.Error(c, fiber.StatusBadRequest, "no_freezes", err.Error())
		case errors.Is(err, streakuc.ErrAlreadyProtected):
			return response.Error(c, fiber.StatusConflict, "already_protected", err.Error())
		case errors.Is(err, streakuc.ErrNoStreak):
			return response.Error(c, fiber.StatusBadRequest, "no_streak", err.Error())
		case errors.Is(err, streakuc.ErrSoftExpired):
			return response.Error(c, fiber.StatusBadRequest, "streak_soft_expired",
				"Streak đã gãy từ trước, không thể dùng bảo vệ nữa.")
		case errors.Is(err, streakuc.ErrCatchUpRequired):
			return response.Error(c, fiber.StatusBadRequest, "catch_up_required",
				"Check-in hôm nay để tiếp tục chuỗi ngày (đã bảo vệ hoặc sẽ tự cứu).")
		default:
			return response.Error(c, fiber.StatusInternalServerError, "streak_freeze_error", err.Error())
		}
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// AdminReconcile handles POST /api/v1/admin/users/:userId/streak/reconcile.
//
// Admin-only repair tool: rebuilds streak counters from SkinCheck history when
// they drifted due to a system bug. Intentionally NOT on /me/* — ordinary users
// must not be able to call this (previously it reset freezes_available and was
// a freeze-refill vector). RequireAdmin must run before this handler.
func (h *StreakHandler) AdminReconcile(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "streak unavailable")
	}
	adminID := middleware.UserIDFromLocals(c)
	if adminID == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	targetID, err := uuid.Parse(strings.TrimSpace(c.Params("userId")))
	if err != nil || targetID == uuid.Nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_user_id", "userId must be a valid UUID")
	}

	adminEmail, _ := c.Locals("auth_user_email").(string)
	slog.Info("streak reconcile: admin request",
		"admin_user_id", adminID,
		"admin_email", adminEmail,
		"target_user_id", targetID,
		"action", "reconcile",
	)

	result, err := h.svc.ReconcileForUser(c.UserContext(), targetID)
	if err != nil {
		slog.Warn("streak reconcile: failed",
			"admin_user_id", adminID,
			"admin_email", adminEmail,
			"target_user_id", targetID,
			"err", err,
		)
		if errors.Is(err, streakuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "streak_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "streak_reconcile_error",
			"failed to reconcile streak from skin check history")
	}

	slog.Info("streak reconcile: admin success",
		"admin_user_id", adminID,
		"admin_email", adminEmail,
		"target_user_id", targetID,
		"days_replayed", result.DaysReplayed,
		"before_current", result.Before.CurrentStreak,
		"after_current", result.After.CurrentStreak,
		"freezes_preserved", result.FreezesPreserved,
		"freeze_bridges_invented", result.FreezeBridgesInvented,
	)

	msg := "Streak counters rebuilt from SkinCheck history."
	if result.FreezesPreserved {
		msg += " Freeze inventory was preserved."
	} else {
		msg += " WARNING: freeze inventory changed unexpectedly."
	}
	if !result.FreezeBridgesInvented {
		msg += " Auto-freeze bridges were not simulated during replay."
	}

	return response.JSON(c, fiber.StatusOK, dto.AdminStreakReconcileResponse{
		Message:               msg,
		UserID:                targetID.String(),
		DaysReplayed:          result.DaysReplayed,
		FreezesPreserved:      result.FreezesPreserved,
		FreezeBridgesInvented: result.FreezeBridgesInvented,
		Note: "Replay uses FreezesAvailable=0 so historical 1-day gaps reset the streak instead of inventing freeze bridges. After reconcile, current/longest may be lower than a live history that had real auto-freezes — that is intentional for honest repair.",
		Before:                result.Before,
		After:                 result.After,
	})
}
