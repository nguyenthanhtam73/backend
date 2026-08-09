package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestDenseInflammatoryAcneMapsCalmFirst(t *testing.T) {
	raw := onboardingVisionRaw{
		SkinObservations: dto.OnboardingSkinObservations{
			OverallSkinType: "combination",
			TZone:           "slightly_oily",
			Cheeks:          "normal",
			PoreSize:        "medium",
			Texture:         "bumpy",
			Redness:         "moderate",
			Pigmentation:    "slight_uneven",
			AcneStatus:      "inflammatory_acne",
			OilinessLevel:   "medium",
		},
		DetailedObservations: "Hai má đang đỏ sưng dày, nhiều nốt viêm. Cằm cũng có vài nốt đỏ. Trán hơi bóng.",
		MainConcerns:         []string{"mụn viêm", "da đỏ"},
		SeverityLevel:        "dense",
		PrimaryRegions:       []string{"má", "cằm"},
		ConcernTypes:         []string{"mụn viêm", "đỏ–kích"},
		Phase:                "can_add_active", // must be forced down to calm_first
		Summary:              "Má viêm dày rõ trên ảnh.",
		SkinTone:             "medium",
		Undertone:            "warm",
		PhotoQuality:         "good",
	}
	out := mapOnboardingVisionRaw(raw, "vi")
	if out.SeverityLevel != SeverityDense {
		t.Fatalf("severity: got %q", out.SeverityLevel)
	}
	if out.Phase != PhaseCalmFirst {
		t.Fatalf("phase must be calm_first for dense flare, got %q", out.Phase)
	}
	if !containsStr(out.PrimaryRegions, "cheeks") {
		t.Fatalf("regions: got %v", out.PrimaryRegions)
	}
	if !containsStr(out.ConcernTypes, "inflammatory_acne") {
		t.Fatalf("concern_types: got %v", out.ConcernTypes)
	}
	if len(out.ProductGuidance) == 0 {
		t.Fatal("expected product_guidance")
	}
	for _, g := range out.ProductGuidance {
		if g.Step == "treat" {
			t.Fatal("calm_first must not include treat/BHA step")
		}
		low := strings.ToLower(g.Why + " " + g.NameOrCategory)
		// Caution may mention "không đẩy BHA/BP" — that is OK. Why/title must not recommend them.
		if strings.Contains(low, "benzoyl") || strings.Contains(low, " dùng bha") || strings.Contains(low, "thêm bha") {
			t.Fatalf("calm_first guidance must not push BHA/BP: %+v", g)
		}
		if g.AffiliateProductID == "some-by-mi-miracle-toner" || g.AffiliateProductID == "to-niacinamide" {
			t.Fatalf("must not affiliate-match actives in calm_first: %s", g.AffiliateProductID)
		}
	}
}

func TestScrubForbiddenSummary(t *testing.T) {
	got := normalizeSummary("Da không quá nặng, 2–3 tuần cải thiện rõ", "Má đỏ.", SeverityDense, "vi")
	if strings.Contains(strings.ToLower(got), "không quá nặng") || strings.Contains(got, "2–3 tuần") {
		t.Fatalf("forbidden claims leaked: %q", got)
	}
}

func TestBuildGuidanceAffiliateLinksFromCatalogOnly(t *testing.T) {
	guidance, suggestions := BuildOnboardingProductGuidance(
		PhaseCalmFirst, SeverityDense, "combo",
		[]string{"acne", "redness"},
		[]string{"inflammatory_acne", "redness_irritation"},
		"vi",
	)
	if len(guidance) == 0 {
		t.Fatal("expected guidance")
	}
	for _, g := range guidance {
		if g.AffiliateLink == "" {
			continue
		}
		if g.AffiliateProductID == "" {
			t.Fatal("link without catalog id")
		}
		if !strings.Contains(g.AffiliateLink, "shopee") {
			t.Fatalf("unexpected link: %s", g.AffiliateLink)
		}
	}
	for _, s := range suggestions {
		if s.AffiliateLink == "" || s.ProductName == "" {
			t.Fatalf("bad suggestion: %+v", s)
		}
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
