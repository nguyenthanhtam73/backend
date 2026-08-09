package profile

import (
	"testing"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/service/ai"
)

func TestApplyClientStarterSteps_OverridesWhenProvided(t *testing.T) {
	starter := ai.StarterRoutine{
		Morning: []string{"scaffold am"},
		Evening: []string{"scaffold pm"},
	}
	req := dto.OnboardingCompleteRequest{
		Morning: []string{"  Custom AM  ", "", "Second AM"},
		Evening: []string{"Custom PM"},
	}
	if !applyClientStarterSteps(&starter, req) {
		t.Fatal("expected client steps to apply")
	}
	if len(starter.Morning) != 2 || starter.Morning[0] != "Custom AM" || starter.Morning[1] != "Second AM" {
		t.Fatalf("morning = %#v", starter.Morning)
	}
	if len(starter.Evening) != 1 || starter.Evening[0] != "Custom PM" {
		t.Fatalf("evening = %#v", starter.Evening)
	}
}

func TestApplyClientStarterSteps_NoOpWhenEmpty(t *testing.T) {
	starter := ai.StarterRoutine{
		Morning: []string{"scaffold am"},
		Evening: []string{"scaffold pm"},
	}
	req := dto.OnboardingCompleteRequest{
		Morning: []string{"  ", ""},
		Evening: nil,
	}
	if applyClientStarterSteps(&starter, req) {
		t.Fatal("expected no client steps")
	}
	if starter.Morning[0] != "scaffold am" || starter.Evening[0] != "scaffold pm" {
		t.Fatalf("scaffold should be unchanged: %#v", starter)
	}
}

func TestSanitizeClientStarterSteps_CapsLength(t *testing.T) {
	in := make([]string, maxClientStarterSteps+5)
	for i := range in {
		in[i] = "step"
	}
	out := sanitizeClientStarterSteps(in)
	if len(out) != maxClientStarterSteps {
		t.Fatalf("len=%d want %d", len(out), maxClientStarterSteps)
	}
}
