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
	if got := mapOnboardingConcernLabel("barrier yếu"); got != "weak_barrier" {
		t.Fatalf("got %q", got)
	}
	if got := mapOnboardingConcernLabel("da dễ đỏ"); got != "weak_barrier" {
		t.Fatalf("da dễ đỏ: got %q", got)
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
