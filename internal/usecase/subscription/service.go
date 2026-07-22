// Package subscription owns Premium lifecycle: trial, cancel, renew, grace, expiry.
//
// Entitlement gates still go through usecase/premium (EffectivePlanTier + grace).
// This package mutates User subscription columns + append-only subscriptions history
// inside DB transactions (FOR UPDATE on the user row).
package subscription

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// systemActorEmail is stored on plan_change_logs for lifecycle-driven tier changes.
const systemActorEmail = "subscription@system.dadiary"

// ActivePlan is the API-facing snapshot returned by CheckActivePlan / mutations.
// Field names are stable for /me + i18n clients (message keys use Status / InGrace).
type ActivePlan struct {
	Active              bool                      `json:"active"`
	PlanTier            domain.PlanTier           `json:"plan_tier"`
	Status              domain.SubscriptionStatus `json:"subscription_status"`
	PlanExpiresAt       *time.Time                `json:"plan_expires_at,omitempty"`
	TrialEndsAt         *time.Time                `json:"trial_ends_at,omitempty"`
	CanceledAt          *time.Time                `json:"canceled_at,omitempty"`
	GraceEndsAt         *time.Time                `json:"grace_ends_at,omitempty"`
	AccessEndsAt        *time.Time                `json:"access_ends_at,omitempty"`
	DaysLeft            int                       `json:"days_left"` // -1 = lifetime; 0 = ended
	InGrace             bool                      `json:"in_grace"`
	CancelAtPeriodEnd   bool                      `json:"cancel_at_period_end"`
	EligibleForTrial    bool                      `json:"eligible_for_trial"`
	TrialDaysConfigured int                       `json:"trial_days,omitempty"`
	GraceDaysConfigured int                       `json:"grace_days,omitempty"`
}

// RenewalInput is the SePay (or admin) paid renewal / first-paid upgrade payload.
type RenewalInput struct {
	UserID          uuid.UUID
	PlanTier        domain.PlanTier
	BillingInterval domain.BillingInterval
	// ExternalRef should be the SePay invoice number (idempotency key).
	ExternalRef string
	Provider    domain.SubscriptionProvider
	Now         time.Time // zero → time.Now().UTC()
}

// Service orchestrates subscription lifecycle mutations.
type Service struct {
	db       *gorm.DB
	users    *repository.GormUserRepository
	subs     *repository.SubscriptionRepository
	logs     *repository.PlanChangeLogRepository
	trialDays int
	graceDays int
}

// NewService wires lifecycle deps. trialDays / graceDays are clamped to product ranges.
func NewService(
	db *gorm.DB,
	users *repository.GormUserRepository,
	subs *repository.SubscriptionRepository,
	logs *repository.PlanChangeLogRepository,
	trialDays, graceDays int,
) *Service {
	return &Service{
		db:        db,
		users:     users,
		subs:      subs,
		logs:      logs,
		trialDays: domain.ClampTrialDays(trialDays),
		graceDays: domain.ClampGraceDays(graceDays),
	}
}

func (s *Service) ready() error {
	if s == nil || s.db == nil || s.users == nil || s.subs == nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) TrialDays() int { return domain.ClampTrialDays(s.trialDays) }
func (s *Service) GraceDays() int { return domain.ClampGraceDays(s.graceDays) }

// StartTrial grants a one-time free trial (7–14 days) on first upgrade.
// Idempotent: returns ErrNotEligible when trial was already used or user is paid.
func (s *Service) StartTrial(
	ctx context.Context,
	userID uuid.UUID,
	planTier domain.PlanTier,
) (*ActivePlan, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrInvalidUser
	}
	planTier = domain.NormalizePlanTier(planTier)
	if !planTier.IsPaidPlan() {
		planTier = domain.PlanPremium // trial defaults to Premium
	}

	now := time.Now().UTC()
	trialDays := s.TrialDays()
	trialEnd := now.Add(domain.DaysDuration(trialDays))
	graceDays := s.GraceDays()

	var out *ActivePlan
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		u, err := s.users.GetByIDForUpdateTx(tx, userID)
		if err != nil {
			return err
		}
		if u == nil {
			return ErrInvalidUser
		}
		if !domain.EligibleForTrial(u, now, graceDays) {
			return ErrNotEligible
		}

		from := domain.NormalizePlanTier(u.PlanTier)
		updated, err := s.users.ApplySubscriptionStateTx(tx, userID, repository.SubscriptionStatePatch{
			PlanTier:           planTier,
			SetPlanTier:        true,
			PlanExpiresAt:      &trialEnd,
			SetPlanExpiresAt:   true,
			TrialEndsAt:        &trialEnd,
			SetTrialEndsAt:     true,
			CanceledAt:         nil,
			SetCanceledAt:      true,
			SubscriptionStatus: domain.SubStatusTrialing,
			SetStatus:          true,
		})
		if err != nil {
			return err
		}
		if updated == nil {
			return ErrInvalidUser
		}

		graceEnd := domain.GraceEndsAt(&trialEnd, graceDays)
		hist := &domain.Subscription{
			UserID:          userID,
			PlanTier:        planTier,
			BillingInterval: "",
			Status:          domain.SubStatusTrialing,
			EventType:       domain.SubEventTrialStarted,
			Provider:        domain.SubProviderTrial,
			TrialEndsAt:     &trialEnd,
			PeriodStartsAt:  &now,
			PeriodEndsAt:    &trialEnd,
			GraceEndsAt:     graceEnd,
		}
		if err := s.subs.CreateTx(tx, hist); err != nil {
			return err
		}
		if err := s.writePlanLogTx(tx, userID, from, planTier, "trial_started"); err != nil {
			return err
		}

		out = s.snapshot(updated, now)
		slog.Info("subscription: trial started",
			"user_id", userID.String(),
			"plan_tier", string(planTier),
			"trial_ends_at", trialEnd.Format(time.RFC3339),
			"trial_days", trialDays,
		)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CancelSubscription marks the subscription canceled_at. Access continues until
// plan_expires_at + grace; the daily cron then downgrades.
func (s *Service) CancelSubscription(ctx context.Context, userID uuid.UUID) (*ActivePlan, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrInvalidUser
	}

	now := time.Now().UTC()
	graceDays := s.GraceDays()
	var out *ActivePlan

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		u, err := s.users.GetByIDForUpdateTx(tx, userID)
		if err != nil {
			return err
		}
		if u == nil {
			return ErrInvalidUser
		}

		effective := domain.EffectivePlanTierWithGrace(u, now, graceDays)
		if !effective.IsPaidPlan() {
			return ErrNotActive
		}
		if u.CanceledAt != nil {
			return ErrAlreadyCanceled
		}

		canceledAt := now
		updated, err := s.users.ApplySubscriptionStateTx(tx, userID, repository.SubscriptionStatePatch{
			CanceledAt:         &canceledAt,
			SetCanceledAt:      true,
			SubscriptionStatus: domain.SubStatusCanceled,
			SetStatus:          true,
		})
		if err != nil {
			return err
		}
		if updated == nil {
			return ErrInvalidUser
		}

		graceEnd := domain.GraceEndsAt(updated.PlanExpiresAt, graceDays)
		hist := &domain.Subscription{
			UserID:         userID,
			PlanTier:       domain.NormalizePlanTier(updated.PlanTier),
			Status:         domain.SubStatusCanceled,
			EventType:      domain.SubEventCanceled,
			Provider:       domain.SubProviderSePay, // self-serve cancel; renewals are SePay-backed
			PeriodEndsAt:   updated.PlanExpiresAt,
			CanceledAt:     &canceledAt,
			GraceEndsAt:    graceEnd,
			TrialEndsAt:    updated.TrialEndsAt,
		}
		if err := s.subs.CreateTx(tx, hist); err != nil {
			return err
		}

		out = s.snapshot(updated, now)
		slog.Info("subscription: canceled",
			"user_id", userID.String(),
			"plan", string(domain.NormalizePlanTier(updated.PlanTier)),
			"plan_expires_at", formatTimePtr(updated.PlanExpiresAt),
			"grace_ends_at", formatTimePtr(graceEnd),
		)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// HandleRenewal extends plan_expires_at after a successful SePay payment (renew or upgrade).
// Clears canceled_at and sets status=active. Idempotent when ExternalRef was already applied.
func (s *Service) HandleRenewal(ctx context.Context, in RenewalInput) (*ActivePlan, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	in = normalizeRenewalInput(in)

	// Fast-path idempotency (also re-checked under the transaction).
	if in.ExternalRef != "" {
		exists, err := s.subs.ExistsByExternalRef(ctx, in.ExternalRef)
		if err != nil {
			return nil, err
		}
		if exists {
			u, err := s.users.GetByID(ctx, in.UserID)
			if err != nil {
				return nil, err
			}
			if u == nil {
				return nil, ErrInvalidUser
			}
			slog.Info("subscription: renewal noop (external_ref seen)",
				"user_id", in.UserID.String(),
				"external_ref", in.ExternalRef,
			)
			return s.snapshot(u, in.Now), nil
		}
	}

	var out *ActivePlan
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		plan, err := s.ApplyRenewalTx(tx, in)
		if err != nil {
			return err
		}
		out = plan
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ApplyRenewalTx applies a paid renew/upgrade inside an open transaction.
// Used by SePay IPN fulfilment so MarkPaid + plan extend commit atomically.
// Caller must already be inside tx; this locks the user row FOR UPDATE.
func (s *Service) ApplyRenewalTx(tx *gorm.DB, in RenewalInput) (*ActivePlan, error) {
	if s == nil || s.users == nil || s.subs == nil {
		return nil, ErrUnavailable
	}
	if tx == nil {
		return nil, fmt.Errorf("%w: transaction required", ErrUnavailable)
	}
	in = normalizeRenewalInput(in)
	if in.UserID == uuid.Nil {
		return nil, ErrInvalidUser
	}
	if !in.PlanTier.IsPaidPlan() {
		return nil, ErrInvalidPlan
	}

	graceDays := s.GraceDays()
	now := in.Now
	extRef := in.ExternalRef

	u, err := s.users.GetByIDForUpdateTx(tx, in.UserID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrInvalidUser
	}

	// Idempotency under lock (concurrent IPNs / nested callers).
	if extRef != "" {
		var count int64
		if err := tx.Model(&domain.Subscription{}).
			Where("external_ref = ?", extRef).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			return s.snapshot(u, now), nil
		}
	}

	fromStored := domain.NormalizePlanTier(u.PlanTier)
	fromEffective := domain.EffectivePlanTierWithGrace(u, now, graceDays)
	to := higherPlan(fromEffective, in.PlanTier)
	expires := domain.ComputePlanExpiry(in.BillingInterval, now, u.PlanExpiresAt)
	periodStart := now

	updated, err := s.users.ApplySubscriptionStateTx(tx, in.UserID, repository.SubscriptionStatePatch{
		PlanTier:           to,
		SetPlanTier:        true,
		PlanExpiresAt:      &expires,
		SetPlanExpiresAt:   true,
		CanceledAt:         nil,
		SetCanceledAt:      true,
		SubscriptionStatus: domain.SubStatusActive,
		SetStatus:          true,
		// Keep TrialEndsAt unchanged (proves trial already used when set).
	})
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrInvalidUser
	}

	graceEnd := domain.GraceEndsAt(&expires, graceDays)
	hist := &domain.Subscription{
		UserID:          in.UserID,
		PlanTier:        to,
		BillingInterval: in.BillingInterval,
		Status:          domain.SubStatusActive,
		EventType:       domain.SubEventRenewed,
		Provider:        in.Provider,
		ExternalRef:     extRef,
		PeriodStartsAt:  &periodStart,
		PeriodEndsAt:    &expires,
		GraceEndsAt:     graceEnd,
		TrialEndsAt:     updated.TrialEndsAt,
	}
	if err := s.subs.CreateTx(tx, hist); err != nil {
		return nil, err
	}
	if fromStored != to {
		if err := s.writePlanLogTx(tx, in.UserID, fromStored, to,
			fmt.Sprintf("renewal:%s", firstNonEmpty(extRef, string(in.Provider)))); err != nil {
			return nil, err
		}
	}

	slog.Info("subscription: renewed",
		"user_id", in.UserID.String(),
		"from", string(fromStored),
		"to", string(to),
		"expires_at", expires.Format(time.RFC3339),
		"external_ref", extRef,
		"interval", string(in.BillingInterval),
	)
	return s.snapshot(updated, now), nil
}

func normalizeRenewalInput(in RenewalInput) RenewalInput {
	in.PlanTier = domain.NormalizePlanTier(in.PlanTier)
	interval := domain.BillingInterval(strings.ToLower(strings.TrimSpace(string(in.BillingInterval))))
	if interval != domain.BillingYearly {
		interval = domain.BillingMonthly
	}
	in.BillingInterval = interval
	if in.Provider == "" {
		in.Provider = domain.SubProviderSePay
	}
	if in.Now.IsZero() {
		in.Now = time.Now().UTC()
	} else {
		in.Now = in.Now.UTC()
	}
	in.ExternalRef = strings.TrimSpace(in.ExternalRef)
	return in
}

// CheckActivePlan returns whether the user currently has Premium access (incl. trial + grace)
// plus days-left / grace flags for /me and pricing UI.
func (s *Service) CheckActivePlan(ctx context.Context, userID uuid.UUID) (*ActivePlan, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrInvalidUser
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrInvalidUser
	}
	return s.snapshot(u, time.Now().UTC()), nil
}

// DowngradePastGrace sets Free for paid users whose grace window ended.
// Safe under FOR UPDATE so concurrent SePay renewals cannot race the downgrade.
// Intended entrypoint for the daily cron (replaces immediate expiry downgrade).
func (s *Service) DowngradePastGrace(ctx context.Context) (int, error) {
	if err := s.ready(); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	graceDays := s.GraceDays()
	candidates, err := s.users.ListPastGracePaidUsers(ctx, now, graceDays, 500)
	if err != nil {
		return 0, err
	}

	downgraded := 0
	for i := range candidates {
		uid := candidates[i].ID
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			u, err := s.users.GetByIDForUpdateTx(tx, uid)
			if err != nil {
				return err
			}
			if u == nil {
				return nil
			}
			// Re-check under lock (renewal IPN may have extended expiry).
			if domain.EffectivePlanTierWithGrace(u, now, graceDays).IsPaidPlan() {
				return nil
			}
			from := domain.NormalizePlanTier(u.PlanTier)
			if !from.IsPaidPlan() {
				return nil
			}

			updated, err := s.users.ApplySubscriptionStateTx(tx, uid, repository.SubscriptionStatePatch{
				PlanTier:           domain.PlanFree,
				SetPlanTier:        true,
				PlanExpiresAt:      nil,
				SetPlanExpiresAt:   true,
				CanceledAt:         nil,
				SetCanceledAt:      true,
				SubscriptionStatus: domain.SubStatusExpired,
				SetStatus:          true,
				// Keep TrialEndsAt — trial is one-time.
			})
			if err != nil {
				return err
			}
			if updated == nil {
				return nil
			}

			hist := &domain.Subscription{
				UserID:      uid,
				PlanTier:    domain.PlanFree,
				Status:      domain.SubStatusExpired,
				EventType:   domain.SubEventExpired,
				Provider:    domain.SubProviderCron,
				TrialEndsAt: updated.TrialEndsAt,
			}
			if err := s.subs.CreateTx(tx, hist); err != nil {
				return err
			}
			if err := s.writePlanLogTx(tx, uid, from, domain.PlanFree, "grace_ended"); err != nil {
				return err
			}
			downgraded++
			return nil
		})
		if err != nil {
			slog.Error("subscription: grace downgrade failed",
				"user_id", uid.String(),
				"error", err,
			)
			continue
		}
	}

	slog.Info("subscription: grace downgrade complete",
		"candidates", len(candidates),
		"downgraded", downgraded,
		"grace_days", graceDays,
	)
	return downgraded, nil
}

func (s *Service) snapshot(u *domain.User, now time.Time) *ActivePlan {
	graceDays := s.GraceDays()
	trialDays := s.TrialDays()
	if u == nil {
		return &ActivePlan{
			Active:              false,
			PlanTier:            domain.PlanFree,
			Status:              domain.SubStatusNone,
			DaysLeft:            0,
			TrialDaysConfigured: trialDays,
			GraceDaysConfigured: graceDays,
		}
	}
	tier := domain.EffectivePlanTierWithGrace(u, now, graceDays)
	status := domain.ResolveSubscriptionStatus(u, now, graceDays)
	graceEnd := domain.GraceEndsAt(u.PlanExpiresAt, graceDays)
	accessEnd := domain.AccessEndsAt(u, graceDays)
	inGrace := domain.InGracePeriod(u, now, graceDays)

	return &ActivePlan{
		Active:              tier.IsPaidPlan(),
		PlanTier:            tier,
		Status:              status,
		PlanExpiresAt:       cloneTime(u.PlanExpiresAt),
		TrialEndsAt:         cloneTime(u.TrialEndsAt),
		CanceledAt:          cloneTime(u.CanceledAt),
		GraceEndsAt:         graceEnd,
		AccessEndsAt:        accessEnd,
		DaysLeft:            domain.DaysLeftUntilAccessEnd(u, now, graceDays),
		InGrace:             inGrace,
		CancelAtPeriodEnd:   u.CanceledAt != nil && tier.IsPaidPlan(),
		EligibleForTrial:    domain.EligibleForTrial(u, now, graceDays),
		TrialDaysConfigured: trialDays,
		GraceDaysConfigured: graceDays,
	}
}

func (s *Service) writePlanLogTx(
	tx *gorm.DB,
	userID uuid.UUID,
	from, to domain.PlanTier,
	reason string,
) error {
	if s.logs == nil || from == to {
		return nil
	}
	return s.logs.CreateTx(tx, &domain.PlanChangeLog{
		UserID:      userID,
		ActorUserID: userID,
		ActorEmail:  systemActorEmail,
		FromPlan:    from,
		ToPlan:      to,
		Reason:      reason,
	})
}

func higherPlan(a, b domain.PlanTier) domain.PlanTier {
	rank := func(t domain.PlanTier) int {
		switch domain.NormalizePlanTier(t) {
		case domain.PlanPremiumPlus:
			return 2
		case domain.PlanPremium:
			return 1
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return domain.NormalizePlanTier(b)
	}
	return domain.NormalizePlanTier(a)
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
