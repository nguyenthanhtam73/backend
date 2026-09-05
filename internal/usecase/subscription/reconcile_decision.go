package subscription

import (
	"time"

	"github.com/dadiary/backend/internal/domain"
)

// ReconcileAction is the entitlement write the billing reconcile should apply.
type ReconcileAction string

const (
	ReconcileNone             ReconcileAction = "none"
	ReconcileExpire           ReconcileAction = "expire"
	ReconcileRefreshFromSub   ReconcileAction = "refresh_from_sub"
	ReconcileRefreshFromOrder ReconcileAction = "refresh_from_order"
	ReconcilePastDue          ReconcileAction = "past_due"
	ReconcileKeepLifetime     ReconcileAction = "keep_lifetime"
)

// EntitlementEvidence is the verified billing evidence for one user.
// CoveringSub / covering paid-order fields must come from real rows — do not invent them.
type EntitlementEvidence struct {
	User *domain.User
	// CoveringSub is the history row with the latest period_ends_at that still
	// covers now (billed window or grace). Nil when none exists.
	CoveringSub *domain.Subscription
	// CoveringOrderExpiry is the stacked paid-order coverage end (ComputePlanExpiry
	// folded in paid_at order). Nil when no paid order still covers.
	CoveringOrderExpiry *time.Time
	CoveringOrderTier   domain.PlanTier
	Now                 time.Time
	GraceDays           int
}

// UserReconcileDecision is the desired users.* entitlement after one reconcile pass.
type UserReconcileDecision struct {
	Action         ReconcileAction
	PlanTier       domain.PlanTier
	Status         domain.SubscriptionStatus
	PlanExpiresAt  *time.Time
	ClearCanceled  bool
	KeepCanceledAt bool
}

// UserLooksEntitled reports whether stored users.* columns claim paid/open access.
// These rows must be selected even when plan_expires_at is still in the future
// (the previous reconcile only listed overdue subs + past plan_expires_at).
func UserLooksEntitled(u *domain.User) bool {
	if u == nil {
		return false
	}
	if domain.NormalizePlanTier(u.PlanTier).IsPaidPlan() {
		return true
	}
	return domain.IsOpenSubscriptionStatus(u.SubscriptionStatus)
}

// IsLifetimeGrant is the admin-grant shape: paid plan_tier + NULL plan_expires_at.
// Admin PUT /users/:id/plan writes this and does not create a subscriptions row.
func IsLifetimeGrant(u *domain.User) bool {
	if u == nil {
		return false
	}
	return domain.NormalizePlanTier(u.PlanTier).IsPaidPlan() && u.PlanExpiresAt == nil
}

// SubscriptionCoversAt is true when a history row still entitles access at now
// (period_ends_at in the future, or inside grace). Status is ignored — a row
// may have been closed while period_ends_at is still valid.
func SubscriptionCoversAt(sub *domain.Subscription, now time.Time, graceDays int) bool {
	if sub == nil || sub.PeriodEndsAt == nil {
		return false
	}
	graceEnd := domain.GraceEndsAt(sub.PeriodEndsAt, graceDays)
	return graceEnd != nil && graceEnd.After(now.UTC())
}

// FoldPaidOrderExpiry stacks paid orders the same way HandleRenewal extends
// plan_expires_at (oldest paid_at first). Returns nil when there are no paid rows.
func FoldPaidOrderExpiry(orders []domain.PaymentOrder) *time.Time {
	var current *time.Time
	for i := range orders {
		o := orders[i]
		if o.Status != domain.PaymentPaid {
			continue
		}
		start := o.CreatedAt.UTC()
		if o.PaidAt != nil && !o.PaidAt.IsZero() {
			start = o.PaidAt.UTC()
		}
		if start.IsZero() {
			continue
		}
		next := domain.ComputePlanExpiry(o.BillingInterval, start, current)
		current = cloneTime(&next)
	}
	return current
}

// PaidOrdersCoverAt reports whether folded paid-order coverage (plus grace) still includes now.
func PaidOrdersCoverAt(orders []domain.PaymentOrder, now time.Time, graceDays int) bool {
	expires := FoldPaidOrderExpiry(orders)
	if expires == nil {
		return false
	}
	graceEnd := domain.GraceEndsAt(expires, graceDays)
	return graceEnd != nil && graceEnd.After(now.UTC())
}

// HighestPaidOrderTier returns the highest paid plan_tier among paid orders.
func HighestPaidOrderTier(orders []domain.PaymentOrder) domain.PlanTier {
	best := domain.PlanFree
	for i := range orders {
		if orders[i].Status != domain.PaymentPaid {
			continue
		}
		best = higherPlan(best, orders[i].PlanTier)
	}
	return best
}

// DecideUserReconcile chooses the users.* write from verified evidence.
//
// Priority (do not invent rows):
//  1. Covering subscription period → refresh user from that row (lifetime grants
//     are left as lifetime — admin path never writes a subscriptions row).
//  2. Covering paid SePay order → refresh user from stacked order expiry.
//  3. Lifetime admin grant (paid + NULL expiry) → keep.
//  4. Stored paid/open claim with no covering evidence → free/expired.
func DecideUserReconcile(ev EntitlementEvidence) UserReconcileDecision {
	now := ev.Now.UTC()
	graceDays := domain.ClampGraceDays(ev.GraceDays)
	u := ev.User
	if u == nil {
		return UserReconcileDecision{Action: ReconcileNone, PlanTier: domain.PlanFree, Status: domain.SubStatusNone}
	}

	lifetime := IsLifetimeGrant(u)

	if ev.CoveringSub != nil && SubscriptionCoversAt(ev.CoveringSub, now, graceDays) {
		if lifetime {
			return lifetimeDecision(u)
		}
		return decisionFromSub(u, ev.CoveringSub, now, graceDays)
	}

	if ev.CoveringOrderExpiry != nil {
		graceEnd := domain.GraceEndsAt(ev.CoveringOrderExpiry, graceDays)
		if graceEnd != nil && graceEnd.After(now) {
			if lifetime {
				return lifetimeDecision(u)
			}
			return decisionFromOrder(u, ev.CoveringOrderTier, ev.CoveringOrderExpiry, now, graceDays)
		}
	}

	if lifetime {
		return lifetimeDecision(u)
	}

	if UserLooksEntitled(u) {
		return UserReconcileDecision{
			Action:        ReconcileExpire,
			PlanTier:      domain.PlanFree,
			Status:        domain.SubStatusExpired,
			PlanExpiresAt: nil,
			ClearCanceled: true,
		}
	}
	return UserReconcileDecision{
		Action:        ReconcileNone,
		PlanTier:      domain.NormalizePlanTier(u.PlanTier),
		Status:        domain.NormalizeSubscriptionStatus(u.SubscriptionStatus),
		PlanExpiresAt: cloneTime(u.PlanExpiresAt),
	}
}

func lifetimeDecision(u *domain.User) UserReconcileDecision {
	status := domain.SubStatusActive
	if u.CanceledAt != nil {
		status = domain.SubStatusCanceled
	}
	d := UserReconcileDecision{
		Action:         ReconcileKeepLifetime,
		PlanTier:       domain.NormalizePlanTier(u.PlanTier),
		Status:         status,
		PlanExpiresAt:  nil,
		KeepCanceledAt: u.CanceledAt != nil,
	}
	if userMatchesDecision(u, d) {
		d.Action = ReconcileKeepLifetime
	}
	return d
}

func decisionFromSub(
	u *domain.User,
	sub *domain.Subscription,
	now time.Time,
	graceDays int,
) UserReconcileDecision {
	expires := cloneTime(sub.PeriodEndsAt)
	tier := domain.NormalizePlanTier(sub.PlanTier)
	if !tier.IsPaidPlan() {
		tier = domain.PlanPremium
	}
	status := statusFromPeriod(u, sub, now, graceDays)
	action := ReconcileRefreshFromSub
	if expires == nil || !expires.After(now) {
		action = ReconcilePastDue
	}
	d := UserReconcileDecision{
		Action:         action,
		PlanTier:       tier,
		Status:         status,
		PlanExpiresAt:  expires,
		ClearCanceled:  status == domain.SubStatusActive || status == domain.SubStatusTrialing,
		KeepCanceledAt: status == domain.SubStatusCanceled,
	}
	if userMatchesDecision(u, d) {
		d.Action = ReconcileNone
	}
	return d
}

func decisionFromOrder(
	u *domain.User,
	tier domain.PlanTier,
	expires *time.Time,
	now time.Time,
	graceDays int,
) UserReconcileDecision {
	tier = domain.NormalizePlanTier(tier)
	if !tier.IsPaidPlan() {
		tier = domain.PlanPremium
	}
	if expires == nil {
		return UserReconcileDecision{Action: ReconcileExpire, PlanTier: domain.PlanFree, Status: domain.SubStatusExpired, ClearCanceled: true}
	}
	end := expires.UTC()
	status := domain.SubStatusActive
	action := ReconcileRefreshFromOrder
	if !end.After(now) {
		status = domain.SubStatusPastDue
		action = ReconcilePastDue
	}
	_ = graceDays
	d := UserReconcileDecision{
		Action:        action,
		PlanTier:      tier,
		Status:        status,
		PlanExpiresAt: cloneTime(&end),
		ClearCanceled: true,
	}
	if userMatchesDecision(u, d) {
		d.Action = ReconcileNone
	}
	return d
}

func statusFromPeriod(u *domain.User, sub *domain.Subscription, now time.Time, graceDays int) domain.SubscriptionStatus {
	if sub == nil || sub.PeriodEndsAt == nil {
		return domain.SubStatusExpired
	}
	expires := sub.PeriodEndsAt.UTC()
	canceled := (sub.CanceledAt != nil) || (u != nil && u.CanceledAt != nil)
	if canceled {
		return domain.SubStatusCanceled
	}
	if expires.After(now) {
		if sub.Status == domain.SubStatusTrialing || sub.EventType == domain.SubEventTrialStarted {
			return domain.SubStatusTrialing
		}
		return domain.SubStatusActive
	}
	graceEnd := domain.GraceEndsAt(sub.PeriodEndsAt, graceDays)
	if graceEnd != nil && graceEnd.After(now) {
		return domain.SubStatusPastDue
	}
	return domain.SubStatusExpired
}

func userMatchesDecision(u *domain.User, d UserReconcileDecision) bool {
	if u == nil {
		return false
	}
	if domain.NormalizePlanTier(u.PlanTier) != domain.NormalizePlanTier(d.PlanTier) {
		return false
	}
	if domain.NormalizeSubscriptionStatus(u.SubscriptionStatus) != domain.NormalizeSubscriptionStatus(d.Status) {
		return false
	}
	if !timesEqualPtr(u.PlanExpiresAt, d.PlanExpiresAt) {
		return false
	}
	if d.ClearCanceled && u.CanceledAt != nil {
		return false
	}
	return true
}

func timesEqualPtr(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.UTC().Equal(b.UTC())
}
