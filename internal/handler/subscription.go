package handler

import (
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	subscriptionuc "github.com/dadiary/backend/internal/usecase/subscription"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// SubscriptionHandler exposes self-serve subscription lifecycle endpoints.
type SubscriptionHandler struct {
	svc *subscriptionuc.Service
}

// NewSubscriptionHandler constructs the handler.
func NewSubscriptionHandler(svc *subscriptionuc.Service) *SubscriptionHandler {
	return &SubscriptionHandler{svc: svc}
}

// Cancel handles POST /api/v1/subscription/cancel (JWT).
// Sets canceled_at; Premium access continues until plan_expires_at + grace.
func (h *SubscriptionHandler) Cancel(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "subscription unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	plan, err := h.svc.CancelSubscription(c.UserContext(), uid)
	if err != nil {
		status, code, msg := subscriptionuc.MapError(err)
		return response.Error(c, status, code, msg)
	}

	// Return the same shape as /me subscription fields for easy FE merge.
	pub := dto.UserPublic{}
	if plan != nil {
		dto.ApplySubscriptionSnapshot(&pub, snapshotToDTO(plan))
		pub.PlanTier = string(plan.PlanTier)
	}
	return response.JSONWithMessage(c, fiber.StatusOK, fiber.Map{
		"subscription": plan,
		"user": fiber.Map{
			"plan_tier":             pub.PlanTier,
			"plan_expires_at":       pub.PlanExpiresAt,
			"subscription_status":   pub.SubscriptionStatus,
			"trial_ends_at":         pub.TrialEndsAt,
			"canceled_at":           pub.CanceledAt,
			"grace_ends_at":         pub.GraceEndsAt,
			"days_left":             pub.DaysLeft,
			"in_grace":              pub.InGrace,
			"cancel_at_period_end":  pub.CancelAtPeriodEnd,
			"eligible_for_trial":    pub.EligibleForTrial,
		},
	}, "subscription_canceled")
}

func snapshotToDTO(plan *subscriptionuc.ActivePlan) dto.SubscriptionSnapshot {
	if plan == nil {
		return dto.SubscriptionSnapshot{}
	}
	return dto.SubscriptionSnapshot{
		Active:            plan.Active,
		PlanTier:          string(plan.PlanTier),
		Status:            string(plan.Status),
		PlanExpiresAt:     plan.PlanExpiresAt,
		TrialEndsAt:       plan.TrialEndsAt,
		CanceledAt:        plan.CanceledAt,
		GraceEndsAt:       plan.GraceEndsAt,
		DaysLeft:          plan.DaysLeft,
		InGrace:           plan.InGrace,
		CancelAtPeriodEnd: plan.CancelAtPeriodEnd,
		EligibleForTrial:  plan.EligibleForTrial,
	}
}
