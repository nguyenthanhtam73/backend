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

func TestApplyClientStarterSteps_OverlaysCoachCopy(t *testing.T) {
	starter := ai.StarterRoutine{
		Morning:       []string{"scaffold am"},
		Evening:       []string{"scaffold pm"},
		WeekNotes:     "scaffold week",
		Encouragement: "scaffold cheer",
	}
	req := dto.OnboardingCompleteRequest{
		Morning:       []string{"Custom AM"},
		Evening:       []string{"Custom PM"},
		WeekNotes:     "  Guest week notes  ",
		Encouragement: "Guest cheer",
		SkinReadback:  "Guest readback",
	}
	if !applyClientStarterSteps(&starter, req) {
		t.Fatal("expected client steps to apply")
	}
	if starter.WeekNotes != "Guest week notes" {
		t.Fatalf("week_notes = %q", starter.WeekNotes)
	}
	if starter.Encouragement != "Guest cheer" {
		t.Fatalf("encouragement = %q", starter.Encouragement)
	}
	if starter.SkinReadback != "Guest readback" {
		t.Fatalf("skin_readback = %q", starter.SkinReadback)
	}
}
