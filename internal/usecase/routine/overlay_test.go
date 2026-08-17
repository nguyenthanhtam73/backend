package routine

import (
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestOverlayStepCompletions_AllowsUntickKeepsTitles(t *testing.T) {
	t.Parallel()
	existing := []dto.RoutineStep{
		{ID: "a", Title: "Cleanser", Category: "cleanser", Completed: true},
	}
	incoming := []dto.RoutineStep{
		{ID: "a", Title: "Dirty rename", Category: "serum", Completed: false},
	}
	got := overlayStepCompletions(existing, incoming)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "Cleanser" {
		t.Fatalf("title leaked dirty rename: %q", got[0].Title)
	}
	if got[0].Completed {
		t.Fatal("untick must persist")
	}
}

func TestOverlayStepCompletions_IgnoresNewSteps(t *testing.T) {
	t.Parallel()
	existing := []dto.RoutineStep{{ID: "a", Title: "Cleanser"}}
	incoming := []dto.RoutineStep{
		{ID: "a", Title: "Cleanser", Completed: true},
		{ID: "b", Title: "New SPF", Completed: true},
	}
	got := overlayStepCompletions(existing, incoming)
	if len(got) != 1 || got[0].ID != "a" || !got[0].Completed {
		t.Fatalf("unexpected %#v", got)
	}
}
