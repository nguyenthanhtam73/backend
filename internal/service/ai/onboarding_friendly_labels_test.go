package ai

import (
	"strings"
	"testing"
)

func TestFriendlySkinTypeVI(t *testing.T) {
	if got := friendlySkinType("combo", "vi"); got != "da hỗn hợp" {
		t.Fatalf("got %q", got)
	}
}

func TestFriendlyUndertoneVI(t *testing.T) {
	if got := friendlyUndertone("warm", "vi"); got != "tone ấm" {
		t.Fatalf("got %q", got)
	}
}

func TestFriendlyConcernPreservesVietnameseLabel(t *testing.T) {
	if got := friendlyConcern("mụn viêm", "vi"); got != "mụn viêm" {
		t.Fatalf("got %q", got)
	}
}

func TestFriendlyConcernMapsID(t *testing.T) {
	if got := friendlyConcern("acne", "vi"); got != "mụn" {
		t.Fatalf("got %q", got)
	}
	if got := friendlyConcern("weak_barrier", "vi"); got != "da dễ đỏ / yếu hơn bình thường" {
		t.Fatalf("weak_barrier: got %q", got)
	}
	if got := friendlyConcern("hyperpigmentation", "vi"); got != "thâm / sạm" {
		t.Fatalf("hyperpigmentation: got %q", got)
	}
}

func TestOnboardingPromptsBanJargonGuidance(t *testing.T) {
	vision := OnboardingSkinVisionPrompt()
	coach := OnboardingCoachSystemPrompt()
	starter := StarterRoutineSystemPrompt()
	for _, blob := range []string{vision, coach, starter} {
		for _, must := range []string{"barrier", "nốt đỏ", "dễ hiểu"} {
			if !strings.Contains(strings.ToLower(blob), strings.ToLower(must)) {
				// "barrier" appears as banned-word guidance; "nốt đỏ"/"dễ hiểu" as preferred language.
				t.Fatalf("prompt missing guidance token %q", must)
			}
		}
	}
}
