package domain

import (
	"testing"
	"time"
)

func TestEffectivePlanTier_Expiry(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)
	past := now.Add(-time.Hour)

	cases := []struct {
		name string
		u    *User
		want PlanTier
	}{
		{"nil user", nil, PlanFree},
		{"free", &User{PlanTier: PlanFree}, PlanFree},
		{"premium lifetime", &User{PlanTier: PlanPremium}, PlanPremium},
		{"premium active", &User{PlanTier: PlanPremium, PlanExpiresAt: &future}, PlanPremium},
		{"premium expired", &User{PlanTier: PlanPremium, PlanExpiresAt: &past}, PlanFree},
		{"plus expired", &User{PlanTier: PlanPremiumPlus, PlanExpiresAt: &past}, PlanFree},
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
