package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestMapOnboardingVisionRaw(t *testing.T) {
	raw := onboardingVisionRaw{
		SkinObservations: dto.OnboardingSkinObservations{
			OverallSkinType: "combination",
			TZone:           "very_oily",
			Cheeks:          "dry",
			PoreSize:        "large",
			Texture:         "slightly_rough",
			Redness:         "mild",
			Pigmentation:    "hyperpigmentation",
			AcneStatus:      "inflammatory_acne",
			OilinessLevel:   "high",
		},
		DetailedObservations: "Trán bóng dầu nhẹ. Mũi có lỗ chân lông to. Má trái hồng nhẹ. Cằm có 2 nốt đỏ nhỏ. Da nâu vừa, tone ấm.",
		MainConcerns:         []string{"nốt đỏ", "thâm"},
		SkinTone:             "medium",
		Undertone:            "warm",
		PhotoQuality:         "good",
	}

	out := mapOnboardingVisionRaw(raw, "vi")
	if out.SkinTypeGuess != "combo" {
		t.Fatalf("skin type: got %q", out.SkinTypeGuess)
	}
	if out.UndertoneGuess != "warm" {
		t.Fatalf("undertone: got %q", out.UndertoneGuess)
	}
	if len(out.Concerns) < 2 {
		t.Fatalf("expected mapped concerns, got %v", out.Concerns)
	}
	if out.Concerns[0] != "acne" {
		t.Fatalf("primary concern: got %q", out.Concerns[0])
	}
	if out.SuggestedGoal != "clear_acne" {
		t.Fatalf("goal: got %q", out.SuggestedGoal)
	}
	if !out.PhotoQuality.Sufficient || out.Confidence < 0.8 {
		t.Fatalf("photo quality mapping failed: sufficient=%v confidence=%v", out.PhotoQuality.Sufficient, out.Confidence)
	}
	if len(out.VisualObservations) < 4 {
		t.Fatalf("expected visual bullets, got %v", out.VisualObservations)
	}
	if out.DetailedObservations == "" || out.SkinObservations == nil {
		t.Fatal("expected detailed + structured observations on response")
	}
}

func TestMapOnboardingConcernLabel(t *testing.T) {
	if got := mapOnboardingConcernLabel("Mụn viêm"); got != "acne" {
		t.Fatalf("got %q", got)
	}
	if got := mapOnboardingConcernLabel("nốt đỏ"); got != "acne" {
		t.Fatalf("nốt đỏ: got %q", got)
	}
	if got := mapOnboardingConcernLabel("mụn ẩn"); got != "acne" {
		t.Fatalf("mụn ẩn profile concern: got %q", got)
	}
	if got := mapOnboardingConcernLabel("barrier yếu"); got != "weak_barrier" {
		t.Fatalf("got %q", got)
	}
	if got := mapOnboardingConcernLabel("da dễ đỏ"); got != "weak_barrier" {
		t.Fatalf("da dễ đỏ: got %q", got)
	}
}

func TestMapConcernTypeLabel_ClosedComedonesNotInflammatory(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"mụn ẩn", "Mụn ẩn", "closed comedones", "mụn cồi", "whiteheads"} {
		if got := mapConcernTypeLabel(label); got != "comedones" {
			t.Fatalf("mapConcernTypeLabel(%q)=%q want comedones", label, got)
		}
	}
	if got := mapConcernTypeLabel("mụn viêm"); got != "inflammatory_acne" {
		t.Fatalf("mụn viêm must stay inflammatory_acne, got %q", got)
	}
}

func TestMapConcernTypeLabel_ShortAliasNoFalsePositive(t *testing.T) {
	t.Parallel()
	// "kho" must not match inside "khỏe"; "dau" must not match inside unrelated words.
	if got := mapConcernTypeLabel("da khỏe hơn"); got == "dryness" {
		t.Fatalf("khỏe must not map to dryness, got %q", got)
	}
	if got := mapConcernTypeLabel("khô"); got != "dryness" {
		t.Fatalf("khô exact should be dryness, got %q", got)
	}
	if got := mapConcernTypeLabel("da dầu"); got != "oiliness" {
		t.Fatalf("da dầu should be oiliness, got %q", got)
	}
	if got := mapConcernTypeLabel("đầu đen"); got != "comedones" {
		t.Fatalf("đầu đen should be comedones, got %q", got)
	}
}

func TestNormalizeConcernTypes_MunAn(t *testing.T) {
	t.Parallel()
	obs := dto.OnboardingSkinObservations{
		Texture:    "bumpy",
		AcneStatus: "few_whiteheads",
		Redness:    "none",
	}
	got := normalizeConcernTypes(nil, []string{"mụn ẩn", "thâm"}, obs)
	if !containsStr(got, "comedones") {
		t.Fatalf("expected comedones from mụn ẩn, got %v", got)
	}
	if containsStr(got, "inflammatory_acne") {
		t.Fatalf("mụn ẩn must not map to inflammatory_acne, got %v", got)
	}
	if !containsStr(got, "pih") {
		t.Fatalf("expected pih from thâm, got %v", got)
	}
}

func TestOnboardingPrompt_ClosedComedonesFrame(t *testing.T) {
	t.Parallel()
	p := OnboardingSkinVisionPrompt()
	for _, needle := range []string{"mụn ẩn", "closed comedones", "CẤM phủ nhận", "comedones"} {
		if !strings.Contains(p, needle) {
			t.Fatalf("vision prompt missing %q", needle)
		}
	}
	coach := OnboardingCoachSystemPrompt()
	if !strings.Contains(coach, "mụn ẩn") {
		t.Fatal("coach prompt must mention mụn ẩn")
	}
}

func TestOnboardingPrompt_RedBumpsCalmFirst(t *testing.T) {
	t.Parallel()
	p := OnboardingSkinVisionPrompt()
	for _, needle := range []string{
		"Nốt nhỏ + đỏ hồng rõ",
		"vừa mụn ẩn vừa kích ứng",
		"đẩy BHA/retinol",
	} {
		if !strings.Contains(p, needle) {
			t.Fatalf("onboarding vision missing %q", needle)
		}
	}
	coach := OnboardingCoachSystemPrompt()
	if !strings.Contains(coach, "mụn ẩn + kích ứng/viêm nhẹ") {
		t.Fatal("coach must frame red + tiny bumps as irritation + mụn ẩn")
	}
	obs := dto.OnboardingSkinObservations{
		AcneStatus: "few_whiteheads",
		Redness:    "mild",
		Texture:    "bumpy",
	}
	if got := derivePhase(SeverityMild, obs); got != PhaseCalmFirst {
		t.Fatalf("few_whiteheads + mild redness → calm_first, got %q", got)
	}
}

func TestFriendlyVisualObsBulletsNoJargon(t *testing.T) {
	obs := dto.OnboardingSkinObservations{
		TZone: "very_oily", Texture: "slightly_rough", Redness: "mild",
		Pigmentation: "hyperpigmentation", AcneStatus: "inflammatory_acne",
	}
	bullets := buildStructuredVisualBullets(obs, "vi")
	joined := strings.Join(bullets, " | ")
	for _, bad := range []string{"T-zone", "Texture", "hyperpigmentation", "inflammatory_acne", "very_oily", "barrier"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("jargon %q leaked into bullets: %s", bad, joined)
		}
	}
	if len(bullets) < 3 {
		t.Fatalf("expected friendly bullets, got %v", bullets)
	}
}
