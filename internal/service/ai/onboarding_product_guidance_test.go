package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestCatalogHasCalmFirstCoverage(t *testing.T) {
	rows, err := loadAffiliateCatalog()
	if err != nil {
		t.Fatal(err)
	}
	need := map[string]bool{"cleanse": false, "moisturize": false, "spf": false}
	for _, r := range rows {
		if !phaseAllowedOnEntry(r.Phases, PhaseCalmFirst) {
			continue
		}
		if isActiveHeavyCatalogEntry(r) || catalogStep(r) == "treat" {
			continue
		}
		step := catalogStep(r)
		if _, ok := need[step]; ok {
			need[step] = true
		}
	}
	for step, ok := range need {
		if !ok {
			t.Fatalf("calm_first missing SKU for step %s", step)
		}
	}
}

func TestCanAddActiveHasTreatBHA(t *testing.T) {
	rows, err := loadAffiliateCatalog()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if catalogStep(r) == "treat" && phaseAllowedOnEntry(r.Phases, PhaseCanAddActive) && normLower(r.ActiveKind) == "bha" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected at least one BHA treat SKU for can_add_active")
	}
}

func TestFilterSuggestionsDropsTreatOnCalmFirst(t *testing.T) {
	guidance, picks := BuildOnboardingProductGuidance(
		PhaseCanAddActive, SeverityMild, "oily",
		[]string{"acne"}, []string{"inflammatory_acne"}, "vi",
	)
	if len(guidance) == 0 {
		t.Fatal("expected guidance")
	}
	hasTreat := false
	for _, g := range guidance {
		if g.Step == "treat" {
			hasTreat = true
		}
	}
	if !hasTreat {
		t.Fatal("can_add_active templates must include treat")
	}
	// Ensure filter drops treat even if present in picks.
	withTreat := append([]dto.ProductSuggestion{}, picks...)
	withTreat = append(withTreat, dto.ProductSuggestion{
		ProductName: "Toner AHA BHA PHA 30 Days Miracle",
		Brand:       "Some By Mi",
		Reason:      "test",
		ProductID:   "some-by-mi-miracle-toner",
		Step:        "treat",
	})
	filtered := FilterProductSuggestionsForPhase(withTreat, PhaseCalmFirst)
	for _, p := range filtered {
		if p.Step == "treat" || p.ProductID == "some-by-mi-miracle-toner" {
			t.Fatalf("calm_first must drop treat: %+v", p)
		}
	}
}

func TestEnrichGuidanceHasWhyBenefitsCaution(t *testing.T) {
	guidance, _ := BuildOnboardingProductGuidance(
		PhaseCalmFirst, SeverityDense, "combo",
		[]string{"acne", "redness"},
		[]string{"inflammatory_acne"},
		"vi",
		"cheeks",
	)
	if len(guidance) == 0 {
		t.Fatal("expected guidance cards")
	}
	if len(guidance) > 3 {
		t.Fatalf("calm_first should fold soothe into moisturize (≤3 cards), got %d", len(guidance))
	}
	seenWhy := map[string]struct{}{}
	for _, g := range guidance {
		if g.Step == "treat" {
			t.Fatal("calm_first must not include treat")
		}
		if g.Step == "soothe" {
			t.Fatal("calm_first must fold soothe into moisturize")
		}
		why := strings.TrimSpace(g.Why)
		if why == "" {
			t.Fatalf("step %s missing why", g.Step)
		}
		low := strings.ToLower(why)
		if strings.HasPrefix(low, "với vùng") || strings.HasPrefix(low, "với má") {
			t.Fatalf("step %s why must not repeat region prefix: %q", g.Step, g.Why)
		}
		if _, dup := seenWhy[low]; dup {
			t.Fatalf("duplicate why across cards: %q", g.Why)
		}
		seenWhy[low] = struct{}{}
		switch g.Step {
		case "cleanse":
			if !strings.Contains(low, "rửa") && !strings.Contains(low, "chà") {
				t.Fatalf("cleanse why should mention gentle cleanse: %q", g.Why)
			}
		case "moisturize":
			if !strings.Contains(low, "dịu") && !strings.Contains(low, "barrier") && !strings.Contains(low, "treat") {
				t.Fatalf("moisturize why should mention soothe/barrier: %q", g.Why)
			}
		case "spf":
			if !strings.Contains(low, "spf") && !strings.Contains(low, "thâm") {
				t.Fatalf("spf why should mention protection/marks: %q", g.Why)
			}
		}
		if len(g.Benefits) < 2 {
			t.Fatalf("step %s needs ≥2 benefits, got %v", g.Step, g.Benefits)
		}
		if strings.TrimSpace(g.HowToUse) == "" {
			t.Fatalf("step %s missing how_to_use", g.Step)
		}
		if strings.TrimSpace(g.Caution) == "" {
			t.Fatalf("step %s missing caution", g.Step)
		}
	}
}

func TestDenseGuidanceHasSPFNotBHA(t *testing.T) {
	guidance, picks := BuildOnboardingProductGuidance(
		PhaseCalmFirst, SeverityDense, "combo",
		[]string{"acne", "redness"},
		[]string{"inflammatory_acne", "redness_irritation"},
		"vi",
	)
	affiliateCTAs := 0
	for _, g := range guidance {
		if g.Step == "treat" {
			t.Fatal("dense calm_first must not include treat")
		}
		if g.AffiliateLink != "" {
			affiliateCTAs++
		}
		if g.AffiliateProductID == "some-by-mi-miracle-toner" {
			t.Fatal("must not match BHA on calm_first")
		}
	}
	if affiliateCTAs > maxProductSuggestions {
		t.Fatalf("affiliate CTAs capped at %d, got %d", maxProductSuggestions, affiliateCTAs)
	}
	if len(picks) > maxProductSuggestions {
		t.Fatalf("suggestions capped at %d, got %d", maxProductSuggestions, len(picks))
	}
	if affiliateCTAs == 0 {
		t.Fatal("expected at least one calm_first catalog CTA when coverage exists")
	}
}

func TestSkinMatchDoesNotWildcardNormal(t *testing.T) {
	if skinMatchesCatalog([]string{"normal"}, "oily") {
		t.Fatal("normal must not match oily")
	}
	if !skinMatchesCatalog([]string{"oily", "combination"}, "oily") {
		t.Fatal("exact oily should match")
	}
	if !skinMatchesCatalog([]string{"combination"}, "combo") {
		t.Fatal("combo should alias to combination")
	}
}

func TestInferCarePhaseTightTokens(t *testing.T) {
	if InferCarePhaseFromUserContext("user mentioned barrier cream and dense fog") != PhaseCanAddActive {
		t.Fatal("vague barrier/dense alone must not force calm_first")
	}
	if InferCarePhaseFromUserContext("severity_dense inflammatory_acne") != PhaseCalmFirst {
		t.Fatal("strong clinical tokens should force calm_first")
	}
	if InferCarePhaseFromUserContext("Tags / concerns: redness") != PhaseCanAddActive {
		t.Fatal("profile redness alone must not force calm_first")
	}
	if InferCarePhaseFromUserContext("skin looks mild and steady with redness and irritated cheeks") != PhaseCalmFirst {
		t.Fatal("acute irritation must force calm_first over mild/steady")
	}
	if InferCarePhaseFromUserContext("routine felt unsteady yesterday") != PhaseCanAddActive {
		t.Fatal("unsteady must not match steady token")
	}
	if InferCarePhaseFromUserContext("VISION_OBS:\nseverity_level: dense inflammatory papules") != PhaseCalmFirst {
		t.Fatal("vision dense cue should force calm_first")
	}
	if InferCarePhaseFromUserContext("SKIN_PROFILE: ok\nVISION_OBS:\nmild redness on cheeks") != PhaseCalmFirst {
		t.Fatal("soft redness in vision should force calm_first")
	}
	ctxTodayThenMemory := "TODAY_CHECK_IN:\nfeeling fine\n## USER_MEMORY\nTags / concerns: redness"
	if InferCarePhaseFromUserContext(ctxTodayThenMemory) != PhaseCanAddActive {
		t.Fatal("profile redness after today block must not force calm_first")
	}
}

func TestManualEnrichSkipsCalmFallback(t *testing.T) {
	guidance, _ := BuildManualProductGuidance("glow", "combo", nil, "vi")
	if len(guidance) == 0 {
		t.Fatal("expected manual guidance")
	}
	for _, g := range guidance {
		if strings.Contains(g.Why, "đang cần làm dịu") {
			t.Fatalf("manual enrich must not invent calm flare context: %q", g.Why)
		}
		if g.Step == "treat" {
			t.Fatal("manual guidance must not include treat")
		}
	}
}

func TestTreatCatalogMatchRequiresBHAorBP(t *testing.T) {
	rows, err := loadAffiliateCatalog()
	if err != nil {
		t.Fatal(err)
	}
	// Inject a non-active treat-step SKU that would otherwise win on category.
	fake := affiliateCatalogEntry{
		ID:       "fake-toner-as-treat",
		Category: "toner",
		Step:     "treat",
		Phases:   []string{PhaseCanAddActive},
		// ActiveKind intentionally empty — must not match treat.
	}
	used := map[string]struct{}{}
	_, ok := matchGuidanceCatalog(
		append([]affiliateCatalogEntry{fake}, rows...),
		"treat", "treatment", PhaseCanAddActive, "oily",
		[]string{"acne"}, []string{"inflammatory_acne"}, used,
	)
	if ok {
		// If a real BHA/BP treat exists it may still match — verify ActiveKind.
		match, ok2 := matchGuidanceCatalog(
			rows, "treat", "treatment", PhaseCanAddActive, "oily",
			[]string{"acne"}, []string{"inflammatory_acne"}, used,
		)
		if ok2 {
			ak := normLower(match.ActiveKind)
			if ak != "bha" && ak != "bp" {
				t.Fatalf("treat match must be bha|bp, got %q id=%s", match.ActiveKind, match.ID)
			}
		}
	}
	// Explicitly: fake without active_kind must never win when alone.
	onlyFake := []affiliateCatalogEntry{fake}
	if _, ok := matchGuidanceCatalog(
		onlyFake, "treat", "treatment", PhaseCanAddActive, "oily",
		[]string{"acne"}, []string{"inflammatory_acne"}, map[string]struct{}{},
	); ok {
		t.Fatal("treat without active_kind bha|bp must not match")
	}
}

func TestStripAffiliateResetsName(t *testing.T) {
	in := []dto.ProductGuidanceItem{{
		Step: "cleanse", Category: "cleanser",
		NameOrCategory: "CeraVe · Foaming Cleanser",
		Brand: "CeraVe", ProductName: "Foaming Cleanser",
		Why: "CeraVe Foaming Cleanser fits oily skin.",
		AffiliateLink: "https://example.com", AffiliateProductID: "x",
	}}
	out := StripAffiliateFromProductGuidance(in, "en")
	if out[0].AffiliateLink != "" || out[0].Brand != "" || out[0].ProductName != "" {
		t.Fatalf("commerce fields must clear: %+v", out[0])
	}
	if strings.Contains(strings.ToLower(out[0].NameOrCategory), "cerave") {
		t.Fatalf("NameOrCategory must not keep brand: %q", out[0].NameOrCategory)
	}
	if strings.Contains(strings.ToLower(out[0].Why), "cerave") {
		t.Fatalf("Why must not keep brand: %q", out[0].Why)
	}
}
