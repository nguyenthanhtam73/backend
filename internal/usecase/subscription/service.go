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
	db        *gorm.DB
	users     *repository.GormUserRepository
	subs      *repository.SubscriptionRepository
	logs      *repository.PlanChangeLogRepository
	orders    *repository.PaymentOrderRepository // optional; paid-order evidence for reconcile
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

// AttachPaymentOrders lets reconcile match paid SePay orders before demoting
// a user who has no covering subscriptions row (legacy fulfill / missing history).
func (s *Service) AttachPaymentOrders(orders *repository.PaymentOrderRepository) {
	if s == nil {
		return
	}
	s.orders = orders
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
		if _, err := s.subs.CloseOpenTx(tx, userID, domain.SubStatusExpired, now); err != nil {
			return err
		}
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
		if _, err := s.subs.MarkOpenStatusesTx(tx, userID,
			[]domain.SubscriptionStatus{domain.SubStatusActive, domain.SubStatusTrialing, domain.SubStatusPastDue},
			domain.SubStatusCanceled, now); err != nil {
			return err
		}
		hist := &domain.Subscription{
			UserID:       userID,
			PlanTier:     domain.NormalizePlanTier(updated.PlanTier),
			Status:       domain.SubStatusCanceled,
			EventType:    domain.SubEventCanceled,
			Provider:     domain.SubProviderSePay, // self-serve cancel; renewals are SePay-backed
			PeriodEndsAt: updated.PlanExpiresAt,
			CanceledAt:   &canceledAt,
			GraceEndsAt:  graceEnd,
			TrialEndsAt:  updated.TrialEndsAt,
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
	if _, err := s.subs.CloseOpenTx(tx, in.UserID, domain.SubStatusExpired, now); err != nil {
		return nil, err
	}
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

// ReconcileResult is the outcome of an idempotent billing repair pass.
type ReconcileResult struct {
	Candidates             int `json:"candidates"`
	SubscriptionRowsClosed int `json:"subscription_rows_closed"`
	UsersExpired           int `json:"users_expired"`
	UsersMarkedPastDue     int `json:"users_marked_past_due"`
	UsersRefreshed         int `json:"users_refreshed"`
	HistoryEventsAppended  int `json:"history_events_appended"`
}

// DowngradePastGrace sets Free for paid users whose grace window ended and
// closes leftover open subscription rows. Safe to re-run (idempotent).
// Prefer ReconcileBillingState when the caller wants the full counter set.
func (s *Service) DowngradePastGrace(ctx context.Context) (int, error) {
	res, err := s.ReconcileBillingState(ctx)
	return res.UsersExpired, err
}

// ReconcileBillingState expires overdue subscription rows and syncs users
// to verified billing evidence (covering subscriptions period, then paid
// orders, then lifetime admin grants). Dated Premium with no covering
// evidence is set back to free/expired.
//
// Safe to re-run: already-consistent rows are no-ops. Intended for:
//   - the daily PlanExpiryJob
//   - API boot (repairs leftover prod rows even if today's cron lock is taken)
//   - cmd/reconcile-subscriptions and POST /admin/billing/reconcile
//
// Lifetime admin grants (paid + NULL plan_expires_at) are left on users;
// only their leftover billed-period subscription rows are closed.
func (s *Service) ReconcileBillingState(ctx context.Context) (ReconcileResult, error) {
	var zero ReconcileResult
	if err := s.ready(); err != nil {
		return zero, err
	}
	now := time.Now().UTC()
	return s.reconcileBillingStateAt(ctx, now)
}

func (s *Service) reconcileBillingStateAt(ctx context.Context, now time.Time) (ReconcileResult, error) {
	var out ReconcileResult
	now = now.UTC()
	graceDays := s.GraceDays()

	ids, err := s.collectReconcileCandidates(ctx, now, graceDays)
	if err != nil {
		return out, err
	}

	out.Candidates = len(ids)
	for uid := range ids {
		res, err := s.reconcileUser(ctx, uid, now, graceDays)
		if err != nil {
			slog.Error("subscription: reconcile user failed",
				"user_id", uid.String(),
				"error", err,
			)
			continue
		}
		out.SubscriptionRowsClosed += res.SubscriptionRowsClosed
		out.UsersExpired += res.UsersExpired
		out.UsersMarkedPastDue += res.UsersMarkedPastDue
		out.UsersRefreshed += res.UsersRefreshed
		out.HistoryEventsAppended += res.HistoryEventsAppended
	}

	slog.Info("subscription: billing reconcile complete",
		"candidates", out.Candidates,
		"subscription_rows_closed", out.SubscriptionRowsClosed,
		"users_expired", out.UsersExpired,
		"users_marked_past_due", out.UsersMarkedPastDue,
		"users_refreshed", out.UsersRefreshed,
		"history_events_appended", out.HistoryEventsAppended,
		"grace_days", graceDays,
	)
	return out, nil
}

func (s *Service) collectReconcileCandidates(
	ctx context.Context,
	now time.Time,
	graceDays int,
) (map[uuid.UUID]struct{}, error) {
	ids := map[uuid.UUID]struct{}{}
	add := func(id uuid.UUID) {
		if id != uuid.Nil {
			ids[id] = struct{}{}
		}
	}

	overdue, err := s.subs.ListOpenOverdue(ctx, now, 2000)
	if err != nil {
		return nil, err
	}
	for i := range overdue {
		add(overdue[i].UserID)
	}

	periodEnded, err := s.users.ListExpiredPaidUsers(ctx, now, 1000)
	if err != nil {
		return nil, err
	}
	for i := range periodEnded {
		add(periodEnded[i].ID)
	}

	// Dated Premium / open status with no covering sub was previously missed
	// because plan_expires_at was still in the future (or status drifted).
	paidLooking, err := s.users.ListPaidLookingUsers(ctx, 2000)
	if err != nil {
		return nil, err
	}
	for i := range paidLooking {
		add(paidLooking[i].ID)
	}

	coveringIDs, err := s.subs.ListCoveringUserIDs(ctx, now, graceDays, 2000)
	if err != nil {
		return nil, err
	}
	for _, id := range coveringIDs {
		add(id)
	}

	if s.orders != nil {
		paidUserIDs, err := s.orders.ListDistinctPaidUserIDs(ctx, 2000)
		if err != nil {
			return nil, err
		}
		for _, id := range paidUserIDs {
			add(id)
		}
	}
	return ids, nil
}

func (s *Service) reconcileUser(
	ctx context.Context,
	uid uuid.UUID,
	now time.Time,
	graceDays int,
) (ReconcileResult, error) {
	var out ReconcileResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		u, err := s.users.GetByIDForUpdateTx(tx, uid)
		if err != nil {
			return err
		}
		if u == nil {
			return nil
		}

		rowStatus := subscriptionRowStatusAfterPeriod(u, now, graceDays)
		closed, err := s.subs.MarkOverdueOpenTx(tx, uid, now, rowStatus)
		if err != nil {
			return err
		}
		out.SubscriptionRowsClosed = int(closed)

		covering, err := s.subs.FindBestCoveringForUserTx(tx, uid, now, graceDays)
		if err != nil {
			return err
		}

		ev := EntitlementEvidence{
			User:        u,
			CoveringSub: covering,
			Now:         now,
			GraceDays:   graceDays,
		}
		if s.orders != nil {
			paidOrders, err := s.orders.ListPaidForUserTx(tx, uid)
			if err != nil {
				return err
			}
			if PaidOrdersCoverAt(paidOrders, now, graceDays) {
				ev.CoveringOrderExpiry = FoldPaidOrderExpiry(paidOrders)
				ev.CoveringOrderTier = HighestPaidOrderTier(paidOrders)
			}
		}

		decision := DecideUserReconcile(ev)
		return s.applyReconcileDecision(tx, u, covering, decision, now, &out)
	})
	return out, err
}

func (s *Service) applyReconcileDecision(
	tx *gorm.DB,
	u *domain.User,
	covering *domain.Subscription,
	decision UserReconcileDecision,
	now time.Time,
	out *ReconcileResult,
) error {
	if u == nil || out == nil {
		return nil
	}
	uid := u.ID
	from := domain.NormalizePlanTier(u.PlanTier)

	if covering != nil &&
		(decision.Action == ReconcileRefreshFromSub || decision.Action == ReconcileNone) &&
		decision.Status == domain.SubStatusActive {
		if _, err := s.subs.MarkCoveringActiveTx(tx, covering.ID, now); err != nil {
			return err
		}
	}

	if decision.Action == ReconcileNone ||
		(decision.Action == ReconcileKeepLifetime && userMatchesDecision(u, decision)) {
		return nil
	}

	if decision.Action == ReconcileKeepLifetime {
		if domain.NormalizeSubscriptionStatus(u.SubscriptionStatus) == decision.Status {
			return nil
		}
		_, err := s.users.ApplySubscriptionStateTx(tx, uid, repository.SubscriptionStatePatch{
			SubscriptionStatus: decision.Status,
			SetStatus:          true,
		})
		return err
	}

	if decision.Action == ReconcileExpire {
		updated, err := s.users.ApplySubscriptionStateTx(tx, uid, repository.SubscriptionStatePatch{
			PlanTier:           domain.PlanFree,
			SetPlanTier:        true,
			PlanExpiresAt:      nil,
			SetPlanExpiresAt:   true,
			CanceledAt:         nil,
			SetCanceledAt:      true,
			SubscriptionStatus: domain.SubStatusExpired,
			SetStatus:          true,
		})
		if err != nil {
			return err
		}
		if updated == nil {
			return nil
		}
		out.UsersExpired = 1
		hasExpired, err := s.subs.HasEventTypeTx(tx, uid, domain.SubEventExpired)
		if err != nil {
			return err
		}
		if !hasExpired {
			if err := s.subs.CreateTx(tx, &domain.Subscription{
				UserID:      uid,
				PlanTier:    domain.PlanFree,
				Status:      domain.SubStatusExpired,
				EventType:   domain.SubEventExpired,
				Provider:    domain.SubProviderCron,
				TrialEndsAt: updated.TrialEndsAt,
			}); err != nil {
				return err
			}
			out.HistoryEventsAppended = 1
		}
		reason := "no_covering_period"
		if domain.NormalizePlanTier(from).IsPaidPlan() && u.PlanExpiresAt != nil {
			graceEnd := domain.GraceEndsAt(u.PlanExpiresAt, s.GraceDays())
			if graceEnd != nil && !graceEnd.After(now) {
				reason = "grace_ended"
			}
		}
		return s.writePlanLogTx(tx, uid, from, domain.PlanFree, reason)
	}

	patch := repository.SubscriptionStatePatch{
		PlanTier:           decision.PlanTier,
		SetPlanTier:        true,
		PlanExpiresAt:      decision.PlanExpiresAt,
		SetPlanExpiresAt:   true,
		SubscriptionStatus: decision.Status,
		SetStatus:          true,
	}
	if decision.ClearCanceled {
		patch.CanceledAt = nil
		patch.SetCanceledAt = true
	}
	updated, err := s.users.ApplySubscriptionStateTx(tx, uid, patch)
	if err != nil {
		return err
	}
	if updated == nil {
		return nil
	}
	if decision.Status == domain.SubStatusPastDue &&
		domain.NormalizeSubscriptionStatus(u.SubscriptionStatus) != domain.SubStatusPastDue {
		out.UsersMarkedPastDue = 1
	}
	if from != decision.PlanTier || !timesEqualPtr(u.PlanExpiresAt, decision.PlanExpiresAt) {
		out.UsersRefreshed = 1
		reason := "reconcile_from_sub"
		if decision.Action == ReconcileRefreshFromOrder {
			reason = "reconcile_from_order"
		}
		return s.writePlanLogTx(tx, uid, from, decision.PlanTier, reason)
	}
	return nil
}

// subscriptionRowStatusAfterPeriod is the status an overdue open row should
// take: expired after grace, canceled if the user canceled, otherwise past_due.
func subscriptionRowStatusAfterPeriod(u *domain.User, now time.Time, graceDays int) domain.SubscriptionStatus {
	if u == nil {
		return domain.SubStatusExpired
	}
	resolved := domain.ResolveSubscriptionStatus(u, now, graceDays)
	switch resolved {
	case domain.SubStatusCanceled:
		return domain.SubStatusCanceled
	case domain.SubStatusPastDue:
		return domain.SubStatusPastDue
	case domain.SubStatusActive, domain.SubStatusTrialing:
		// User still has a current period (or lifetime). Overdue leftover
		// rows from an earlier invoice should be marked expired.
		return domain.SubStatusExpired
	default:
		return domain.SubStatusExpired
	}
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
