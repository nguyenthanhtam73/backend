package domain

import (
	"testing"
	"time"
)

func TestEffectivePlanTier_ExpiryAndGrace(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)
	inGrace := now.Add(-2 * time.Hour)                         // expired but inside 3d grace
	pastGrace := now.Add(-(DefaultGraceDays + 1) * 24 * time.Hour) // past grace

	cases := []struct {
		name string
		u    *User
		want PlanTier
	}{
		{"nil user", nil, PlanFree},
		{"free", &User{PlanTier: PlanFree}, PlanFree},
		{"premium lifetime", &User{PlanTier: PlanPremium}, PlanPremium},
		{"premium active", &User{PlanTier: PlanPremium, PlanExpiresAt: &future}, PlanPremium},
		{"premium in grace", &User{PlanTier: PlanPremium, PlanExpiresAt: &inGrace}, PlanPremium},
		{"premium past grace", &User{PlanTier: PlanPremium, PlanExpiresAt: &pastGrace}, PlanFree},
		{"plus past grace", &User{PlanTier: PlanPremiumPlus, PlanExpiresAt: &pastGrace}, PlanFree},
		{"plus active", &User{PlanTier: PlanPremiumPlus, PlanExpiresAt: &future}, PlanPremiumPlus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectivePlanTier(tc.u, now); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestResolveSubscriptionStatus(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	future := now.Add(10 * 24 * time.Hour)
	inGrace := now.Add(-24 * time.Hour)
	canceledAt := now.Add(-time.Hour)
	trialEnd := now.Add(5 * 24 * time.Hour)

	cases := []struct {
		name string
		u    *User
		want SubscriptionStatus
	}{
		{
			name: "active",
			u:    &User{PlanTier: PlanPremium, PlanExpiresAt: &future, SubscriptionStatus: SubStatusActive},
			want: SubStatusActive,
		},
		{
			name: "trialing",
			u: &User{
				PlanTier:           PlanPremium,
				PlanExpiresAt:      &trialEnd,
				TrialEndsAt:        &trialEnd,
				SubscriptionStatus: SubStatusTrialing,
			},
			want: SubStatusTrialing,
		},
		{
			name: "canceled still in period",
			u: &User{
				PlanTier:           PlanPremium,
				PlanExpiresAt:      &future,
				CanceledAt:         &canceledAt,
				SubscriptionStatus: SubStatusCanceled,
			},
			want: SubStatusCanceled,
		},
		{
			name: "past_due grace",
			u: &User{
				PlanTier:           PlanPremium,
				PlanExpiresAt:      &inGrace,
				SubscriptionStatus: SubStatusActive,
			},
			want: SubStatusPastDue,
		},
		{
			name: "free none",
			u:    &User{PlanTier: PlanFree, SubscriptionStatus: SubStatusNone},
			want: SubStatusNone,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveSubscriptionStatus(tc.u, now, DefaultGraceDays); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestComputePlanExpiry(t *testing.T) {
	now := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)

	monthly := ComputePlanExpiry(BillingMonthly, now, nil)
	if !monthly.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("monthly: %s", monthly)
	}
	yearly := ComputePlanExpiry(BillingYearly, now, nil)
	if !yearly.Equal(now.Add(365 * 24 * time.Hour)) {
		t.Fatalf("yearly: %s", yearly)
	}

	// Renewal extends from remaining expiry, not from now.
	current := now.Add(10 * 24 * time.Hour)
	renewed := ComputePlanExpiry(BillingMonthly, now, &current)
	want := current.Add(30 * 24 * time.Hour)
	if !renewed.Equal(want) {
		t.Fatalf("renewal: got %s want %s", renewed, want)
	}

	// Past expiry does not extend from the stale date.
	past := now.Add(-5 * 24 * time.Hour)
	fromNow := ComputePlanExpiry(BillingMonthly, now, &past)
	if !fromNow.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("expired renew: %s", fromNow)
	}
}

func TestEligibleForTrial(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(24 * time.Hour)
	trialUsed := now.Add(-30 * 24 * time.Hour)

	if !EligibleForTrial(&User{PlanTier: PlanFree}, now, DefaultGraceDays) {
		t.Fatal("free never-trial should be eligible")
	}
	if EligibleForTrial(&User{PlanTier: PlanFree, TrialEndsAt: &trialUsed}, now, DefaultGraceDays) {
		t.Fatal("trial already used")
	}
	if EligibleForTrial(&User{PlanTier: PlanPremium, PlanExpiresAt: &future}, now, DefaultGraceDays) {
		t.Fatal("active paid should not be eligible")
	}
}

func TestClampTrialAndGrace(t *testing.T) {
	if ClampTrialDays(0) != DefaultTrialDays {
		t.Fatalf("trial default: %d", ClampTrialDays(0))
	}
	if ClampTrialDays(20) != MaxTrialDays {
		t.Fatalf("trial max: %d", ClampTrialDays(20))
	}
	if ClampGraceDays(1) != DefaultGraceDays {
		t.Fatalf("grace default: %d", ClampGraceDays(1))
	}
	if ClampGraceDays(10) != MaxGraceDays {
		t.Fatalf("grace max: %d", ClampGraceDays(10))
	}
}
