package premium

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
)

func TestEntitlementFor_FreeQuotas(t *testing.T) {
	suggest := EntitlementFor(domain.PlanFree, domain.FeatureAIRoutineSuggestion)
	if suggest.Kind != EntitlementMonthlyQuota || suggest.MonthlyLimit != 3 {
		t.Fatalf("free suggest: %+v", suggest)
	}
	edit := EntitlementFor(domain.PlanFree, domain.FeatureEditRoutine)
	if edit.MonthlyLimit != 5 {
		t.Fatalf("free edit: %+v", edit)
	}
	wardrobe := EntitlementFor(domain.PlanFree, domain.FeatureWardrobeFull)
	if wardrobe.Enabled {
		t.Fatal("free wardrobe should be off")
	}
	hist := EntitlementFor(domain.PlanFree, domain.FeatureProgressFullHistory)
	if hist.HistoryMonths != 3 {
		t.Fatalf("free history: %+v", hist)
	}
}

func TestEntitlementFor_PremiumVsPlus(t *testing.T) {
	premAdv := EntitlementFor(domain.PlanPremium, domain.FeatureAdvancedSkinAnalysis)
	if premAdv.Enabled {
		t.Fatal("premium should not include advanced skin analysis")
	}
	plusAdv := EntitlementFor(domain.PlanPremiumPlus, domain.FeatureAdvancedSkinAnalysis)
	if !plusAdv.Enabled {
		t.Fatal("premium+ should include advanced skin analysis")
	}
	plusHist := EntitlementFor(domain.PlanPremiumPlus, domain.FeatureProgressFullHistory)
	if plusHist.HistoryMonths != 0 {
		t.Fatalf("premium+ history should be unlimited (0), got %d", plusHist.HistoryMonths)
	}
	premHist := EntitlementFor(domain.PlanPremium, domain.FeatureProgressFullHistory)
	if premHist.HistoryMonths != 12 {
		t.Fatalf("premium history: %+v", premHist)
	}
}

func TestEntitlementFor_UnknownTierFallsBackToFree(t *testing.T) {
	ent := EntitlementFor(domain.PlanTier("gold"), domain.FeatureWardrobeFull)
	if ent.Enabled {
		t.Fatal("unknown tier must fall back to free (wardrobe off)")
	}
}

func TestClampProgressRange(t *testing.T) {
	today := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)

	// Free (3 months ≈ 90 days): "all" → 90.
	days, since := ClampProgressRange(3, 0, today)
	if days != 90 {
		t.Fatalf("free all → days=%d want 90", days)
	}
	if since.Format("2006-01-02") != "2026-04-20" {
		t.Fatalf("free since=%s", since.Format("2006-01-02"))
	}

	// Premium 12 months (360 days): request 365 is capped.
	days, _ = ClampProgressRange(12, 365, today)
	if days != 360 {
		t.Fatalf("premium 365 → days=%d want 360", days)
	}

	// Premium+: unlimited, "all" stays open-ended.
	days, since = ClampProgressRange(0, 0, today)
	if days != 0 || !since.IsZero() {
		t.Fatalf("plus all → days=%d since=%v", days, since)
	}
}

func TestNormalizePaidHelpers(t *testing.T) {
	if !domain.PlanPremium.IsPaidPlan() || !domain.PlanPremiumPlus.IsPaidPlan() {
		t.Fatal("paid tiers")
	}
	if domain.PlanFree.IsPaidPlan() {
		t.Fatal("free is not paid")
	}
	if domain.NormalizePlanTier("PREMIUM_PLUS") != domain.PlanPremiumPlus {
		t.Fatal("normalize premium_plus")
	}
}
