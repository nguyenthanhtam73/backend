package mascot

import (
	"encoding/json"
	"testing"
)

func TestMarksFromOnboardingCheeksAcneStaysLight(t *testing.T) {
	snapshot := json.RawMessage(`{
		"skin_analysis": {
			"concerns": ["acne"],
			"severity_level": "mild",
			"primary_regions": ["cheeks"],
			"main_concerns": ["acne"]
		}
	}`)
	got := MarksFromOnboarding(snapshot, nil)
	if len(got) != 1 || got[0].ID != "acne" || got[0].Level != levelLight {
		t.Fatalf("marks %#v", got)
	}
}

func TestMarksFromOnboardingCapsDenseAtMedium(t *testing.T) {
	snapshot := json.RawMessage(`{
		"skin_analysis": {
			"severity_level": "dense",
			"main_concerns": ["inflammatory_acne", "redness"],
			"skin_observations": {
				"acne_status": "active_dense",
				"oiliness_level": "moderate",
				"redness": "high",
				"pigmentation": "mild"
			}
		}
	}`)
	got := MarksFromOnboarding(snapshot, nil)
	want := []string{"acne", "red", "dark_spot", "oily"}
	if len(got) != len(want) {
		t.Fatalf("marks %#v", got)
	}
	for i, id := range want {
		if got[i].ID != id || got[i].Level != levelMedium {
			t.Fatalf("marks %#v", got)
		}
	}
}

func TestMarksFromOnboardingIgnoresClearSkin(t *testing.T) {
	snapshot := json.RawMessage(`{
		"skin_analysis": {
			"concerns": ["normal"],
			"skin_observations": {"acne_status": "none", "redness": "không", "pigmentation": "clear"}
		}
	}`)
	if got := MarksFromOnboarding(snapshot, nil); len(got) != 0 {
		t.Fatalf("marks %#v", got)
	}
}
