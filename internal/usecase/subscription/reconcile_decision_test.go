package subscription

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func testNow() time.Time {
	return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestUserLooksEntitled(t *testing.T) {
	t.Parallel()
	future := testNow().Add(10 * 24 * time.Hour)
	cases := []struct {
		name string
		u    *domain.User
		want bool
	}{
		{name: "nil", u: nil, want: false},
		{name: "free none", u: &domain.User{PlanTier: domain.PlanFree, SubscriptionStatus: domain.SubStatusNone}, want: false},
		{name: "free expired", u: &domain.User{PlanTier: domain.PlanFree, SubscriptionStatus: domain.SubStatusExpired}, want: false},
		{name: "premium future expiry", u: &domain.User{PlanTier: domain.PlanPremium, PlanExpiresAt: &future, SubscriptionStatus: domain.SubStatusActive}, want: true},
		{name: "premium plus expired status", u: &domain.User{PlanTier: domain.PlanPremiumPlus, SubscriptionStatus: domain.SubStatusExpired}, want: true},
		{name: "free but active status", u: &domain.User{PlanTier: domain.PlanFree, SubscriptionStatus: domain.SubStatusActive}, want: true},
		{name: "free past_due", u: &domain.User{PlanTier: domain.PlanFree, SubscriptionStatus: domain.SubStatusPastDue}, want: true},
		{name: "lifetime premium", u: &domain.User{PlanTier: domain.PlanPremium, PlanExpiresAt: nil, SubscriptionStatus: domain.SubStatusActive}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := UserLooksEntitled(tc.u); got != tc.want {
				t.Fatalf("UserLooksEntitled=%v want %v", got, tc.want)
			}
		})
	}
}

func TestSubscriptionCoversAt(t *testing.T) {
	t.Parallel()
	now := testNow()
	inPeriod := now.Add(5 * 24 * time.Hour)
	endedYesterday := now.Add(-24 * time.Hour)
	endedWeekAgo := now.Add(-8 * 24 * time.Hour)
	graceDays := 3

	if SubscriptionCoversAt(nil, now, graceDays) {
		t.Fatal("nil sub must not cover")
	}
	if SubscriptionCoversAt(&domain.Subscription{Status: domain.SubStatusActive}, now, graceDays) {
		t.Fatal("nil period_ends_at must not cover")
	}
	if !SubscriptionCoversAt(&domain.Subscription{
		Status:       domain.SubStatusExpired, // status ignored
		PeriodEndsAt: &inPeriod,
	}, now, graceDays) {
		t.Fatal("future period_ends_at must cover even when status was closed")
	}
	if !SubscriptionCoversAt(&domain.Subscription{
		Status:       domain.SubStatusPastDue,
		PeriodEndsAt: &endedYesterday,
	}, now, graceDays) {
		t.Fatal("ended yesterday is still inside 3-day grace")
	}
	if SubscriptionCoversAt(&domain.Subscription{
		Status:       domain.SubStatusActive,
		PeriodEndsAt: &endedWeekAgo,
	}, now, graceDays) {
		t.Fatal("ended a week ago is past grace")
	}
}

func TestFoldPaidOrderExpiry_StacksLikeRenewal(t *testing.T) {
	t.Parallel()
	now := testNow()
	firstPaid := now.Add(-40 * 24 * time.Hour)
	secondPaid := now.Add(-5 * 24 * time.Hour)
	uid := uuid.New()

	if FoldPaidOrderExpiry(nil) != nil {
		t.Fatal("no orders → nil expiry")
	}
	pending := []domain.PaymentOrder{{
		UserID: uid, Status: domain.PaymentPending, BillingInterval: domain.BillingMonthly, PaidAt: &firstPaid,
	}}
	if FoldPaidOrderExpiry(pending) != nil {
		t.Fatal("pending orders must not invent coverage")
	}

	one := []domain.PaymentOrder{{
		UserID:          uid,
		Status:          domain.PaymentPaid,
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		PaidAt:          &firstPaid,
	}}
	endOne := FoldPaidOrderExpiry(one)
	if endOne == nil {
		t.Fatal("one paid monthly should produce expiry")
	}
	wantOne := domain.ComputePlanExpiry(domain.BillingMonthly, firstPaid, nil)
	if !endOne.Equal(wantOne) {
		t.Fatalf("single fold=%s want %s", endOne.Format(time.RFC3339), wantOne.Format(time.RFC3339))
	}
	if PaidOrdersCoverAt(one, now, 3) {
		t.Fatal("40-day-old monthly + 3d grace must not still cover")
	}

	stacked := []domain.PaymentOrder{
		{UserID: uid, Status: domain.PaymentPaid, PlanTier: domain.PlanPremium, BillingInterval: domain.BillingMonthly, PaidAt: &firstPaid},
		{UserID: uid, Status: domain.PaymentPaid, PlanTier: domain.PlanPremium, BillingInterval: domain.BillingMonthly, PaidAt: &secondPaid},
	}
	endStacked := FoldPaidOrderExpiry(stacked)
	if endStacked == nil {
		t.Fatal("stacked paid orders should produce expiry")
	}
	wantStacked := domain.ComputePlanExpiry(domain.BillingMonthly, secondPaid, &wantOne)
	if !endStacked.Equal(wantStacked) {
		t.Fatalf("stacked fold=%s want %s", endStacked.Format(time.RFC3339), wantStacked.Format(time.RFC3339))
	}
	if !PaidOrdersCoverAt(stacked, now, 3) {
		t.Fatal("stacked renewals should still cover now")
	}
	if HighestPaidOrderTier(stacked) != domain.PlanPremium {
		t.Fatalf("tier=%s", HighestPaidOrderTier(stacked))
	}
}

func TestDecideUserReconcile_SelectionTable(t *testing.T) {
	t.Parallel()
	now := testNow()
	future := now.Add(20 * 24 * time.Hour)
	endedYesterday := now.Add(-24 * time.Hour)
	endedWeekAgo := now.Add(-10 * 24 * time.Hour)
	orderCover := now.Add(12 * 24 * time.Hour)

	paidActive := &domain.User{
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      &future,
		SubscriptionStatus: domain.SubStatusActive,
	}
	orphanFuture := &domain.User{
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      &future,
		SubscriptionStatus: domain.SubStatusActive,
	}
	lifetime := &domain.User{
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      nil,
		SubscriptionStatus: domain.SubStatusActive,
	}
	freeNone := &domain.User{
		PlanTier:           domain.PlanFree,
		SubscriptionStatus: domain.SubStatusNone,
	}
	freeActiveStatus := &domain.User{
		PlanTier:           domain.PlanFree,
		SubscriptionStatus: domain.SubStatusActive,
	}
	covering := &domain.Subscription{
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusActive,
		EventType:    domain.SubEventRenewed,
		PeriodEndsAt: &future,
	}
	coveringExpiredStatus := &domain.Subscription{
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusExpired,
		EventType:    domain.SubEventRenewed,
		PeriodEndsAt: &future,
	}
	inGraceSub := &domain.Subscription{
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusPastDue,
		EventType:    domain.SubEventRenewed,
		PeriodEndsAt: &endedYesterday,
	}
	deadSub := &domain.Subscription{
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusExpired,
		EventType:    domain.SubEventExpired,
		PeriodEndsAt: &endedWeekAgo,
	}

	cases := []struct {
		name   string
		ev     EntitlementEvidence
		action ReconcileAction
		tier   domain.PlanTier
		status domain.SubscriptionStatus
	}{
		{
			name: "already matching covering sub → none",
			ev: EntitlementEvidence{
				User: paidActive, CoveringSub: covering, Now: now, GraceDays: 3,
			},
			action: ReconcileNone, tier: domain.PlanPremium, status: domain.SubStatusActive,
		},
		{
			name: "free user + covering sub → refresh from sub",
			ev: EntitlementEvidence{
				User: freeNone, CoveringSub: covering, Now: now, GraceDays: 3,
			},
			action: ReconcileRefreshFromSub, tier: domain.PlanPremium, status: domain.SubStatusActive,
		},
		{
			name: "covering sub with closed status still refreshes (valid period_ends_at)",
			ev: EntitlementEvidence{
				User: freeNone, CoveringSub: coveringExpiredStatus, Now: now, GraceDays: 3,
			},
			action: ReconcileRefreshFromSub, tier: domain.PlanPremium, status: domain.SubStatusActive,
		},
		{
			name: "in-grace covering sub → past_due",
			ev: EntitlementEvidence{
				User: paidActive, CoveringSub: inGraceSub, Now: now, GraceDays: 3,
			},
			action: ReconcilePastDue, tier: domain.PlanPremium, status: domain.SubStatusPastDue,
		},
		{
			name: "orphan premium future expiry, no sub, no order → expire",
			ev: EntitlementEvidence{
				User: orphanFuture, Now: now, GraceDays: 3,
			},
			action: ReconcileExpire, tier: domain.PlanFree, status: domain.SubStatusExpired,
		},
		{
			name: "orphan premium + covering paid order → refresh from order",
			ev: EntitlementEvidence{
				User: orphanFuture, CoveringOrderExpiry: &orderCover, CoveringOrderTier: domain.PlanPremium,
				Now: now, GraceDays: 3,
			},
			action: ReconcileRefreshFromOrder, tier: domain.PlanPremium, status: domain.SubStatusActive,
		},
		{
			name: "lifetime grant, no sub, no order → keep",
			ev: EntitlementEvidence{
				User: lifetime, Now: now, GraceDays: 3,
			},
			action: ReconcileKeepLifetime, tier: domain.PlanPremium, status: domain.SubStatusActive,
		},
		{
			name: "lifetime + leftover covering sub → still keep lifetime (admin path has no sub row)",
			ev: EntitlementEvidence{
				User: lifetime, CoveringSub: covering, Now: now, GraceDays: 3,
			},
			action: ReconcileKeepLifetime, tier: domain.PlanPremium, status: domain.SubStatusActive,
		},
		{
			name: "free + active status, no evidence → expire",
			ev: EntitlementEvidence{
				User: freeActiveStatus, Now: now, GraceDays: 3,
			},
			action: ReconcileExpire, tier: domain.PlanFree, status: domain.SubStatusExpired,
		},
		{
			name: "dead sub is not evidence → expire dated premium",
			ev: EntitlementEvidence{
				User: orphanFuture, CoveringSub: deadSub, Now: now, GraceDays: 3,
			},
			action: ReconcileExpire, tier: domain.PlanFree, status: domain.SubStatusExpired,
		},
		{
			name: "already free, no evidence → none",
			ev: EntitlementEvidence{
				User: freeNone, Now: now, GraceDays: 3,
			},
			action: ReconcileNone, tier: domain.PlanFree, status: domain.SubStatusNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideUserReconcile(tc.ev)
			if got.Action != tc.action {
				t.Fatalf("action=%s want %s (decision=%+v)", got.Action, tc.action, got)
			}
			if got.PlanTier != tc.tier {
				t.Fatalf("tier=%s want %s", got.PlanTier, tc.tier)
			}
			if got.Status != tc.status {
				t.Fatalf("status=%s want %s", got.Status, tc.status)
			}
			if tc.action == ReconcileExpire && got.PlanExpiresAt != nil {
				t.Fatalf("expire must clear plan_expires_at, got %v", got.PlanExpiresAt)
			}
			if tc.action == ReconcileKeepLifetime && got.PlanExpiresAt != nil {
				t.Fatalf("lifetime must keep NULL expiry, got %v", got.PlanExpiresAt)
			}
		})
	}
}

func TestDecideUserReconcile_DoesNotUseUserExpiryAlone(t *testing.T) {
	t.Parallel()
	now := testNow()
	future := now.Add(40 * 24 * time.Hour)
	u := &domain.User{
		PlanTier:           domain.PlanPremiumPlus,
		PlanExpiresAt:      &future,
		SubscriptionStatus: domain.SubStatusActive,
	}
	got := DecideUserReconcile(EntitlementEvidence{User: u, Now: now, GraceDays: 3})
	if got.Action != ReconcileExpire {
		t.Fatalf("future users.plan_expires_at without sub/order must expire, got %s", got.Action)
	}
}
