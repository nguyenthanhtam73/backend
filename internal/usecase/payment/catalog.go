package payment

import (
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// Catalog prices in VND — keep in sync with frontend/lib/premium/pricing.ts.
var planPrices = map[domain.PlanTier]struct {
	Monthly     int64
	YearlyTotal int64
}{
	domain.PlanPremium:     {Monthly: 99_000, YearlyTotal: 849_000},
	domain.PlanPremiumPlus: {Monthly: 159_000, YearlyTotal: 1_369_000},
}

// AmountForPlan returns the VND amount charged for a plan + billing interval.
func AmountForPlan(tier domain.PlanTier, interval domain.BillingInterval) (int64, error) {
	tier = domain.NormalizePlanTier(tier)
	row, ok := planPrices[tier]
	if !ok || !tier.IsPaidPlan() {
		return 0, fmt.Errorf("%w: plan_tier must be premium or premium_plus", ErrInvalidRequest)
	}
	switch domain.BillingInterval(strings.ToLower(string(interval))) {
	case domain.BillingMonthly:
		return row.Monthly, nil
	case domain.BillingYearly:
		return row.YearlyTotal, nil
	default:
		return 0, fmt.Errorf("%w: billing_interval must be monthly or yearly", ErrInvalidRequest)
	}
}

func normalizeBillingInterval(raw string) (domain.BillingInterval, error) {
	switch domain.BillingInterval(strings.ToLower(strings.TrimSpace(raw))) {
	case domain.BillingMonthly:
		return domain.BillingMonthly, nil
	case domain.BillingYearly:
		return domain.BillingYearly, nil
	default:
		return "", fmt.Errorf("%w: billing_interval must be monthly or yearly", ErrInvalidRequest)
	}
}

func normalizePaidPlan(raw string) (domain.PlanTier, error) {
	tier := domain.NormalizePlanTier(domain.PlanTier(raw))
	if !tier.IsPaidPlan() {
		return "", fmt.Errorf("%w: plan_tier must be premium or premium_plus", ErrInvalidRequest)
	}
	return tier, nil
}
