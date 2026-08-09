package ai

import (
	"strings"
	"testing"
)

// The no-photo path is the whole point of this builder: it must never return an
// empty section, otherwise onboarding step 2 shows routine steps with no products.
func TestManualGuidanceAlwaysReturnsCards(t *testing.T) {
	goals := []string{"", "unsure", "glow", "clear_acne", "barrier", "anti_aging"}
	skins := []string{"", "prefer_not", "normal", "oily", "dry", "combo", "sensitive"}
	for _, goal := range goals {
		for _, skin := range skins {
			for _, locale := range []string{"vi", "en"} {
				guidance, _ := BuildManualProductGuidance(goal, skin, nil, locale)
				if len(guidance) == 0 {
					t.Fatalf("no guidance for goal=%q skin=%q locale=%q", goal, skin, locale)
				}
				for _, g := range guidance {
					if strings.TrimSpace(g.NameOrCategory) == "" {
						t.Fatalf("empty role label: goal=%q skin=%q step=%q", goal, skin, g.Step)
					}
					if strings.TrimSpace(g.Why) == "" {
						t.Fatalf("empty why: goal=%q skin=%q step=%q", goal, skin, g.Step)
					}
				}
			}
		}
	}
}

func TestManualGuidanceStaysCalmFirst(t *testing.T) {
	guidance, picks := BuildManualProductGuidance(
		"clear_acne", "oily", []string{"acne", "large_pores"}, "vi",
	)
	for _, g := range guidance {
		if g.Step == "treat" {
			t.Fatal("no-photo path must not push an active")
		}
		if g.Phase != PhaseCalmFirst {
			t.Fatalf("phase: got %q want %q", g.Phase, PhaseCalmFirst)
		}
	}
	for _, p := range picks {
		if p.Step == "treat" {
			t.Fatalf("treat SKU leaked without a photo: %+v", p)
		}
	}
}

func TestManualGuidanceCopyIsNotFlareWording(t *testing.T) {
	guidance, _ := BuildManualProductGuidance("glow", "normal", []string{"dullness"}, "vi")
	// calm_first templates assume a visible flare; that copy must not reach a user
	// who never uploaded a photo.
	banned := []string{"đang đỏ", "sưng dày", "mụn viêm dày", "vùng vừa viêm"}
	for _, g := range guidance {
		blob := strings.ToLower(g.Why + " " + g.HowToUse + " " + g.Caution)
		for _, b := range banned {
			if strings.Contains(blob, b) {
				t.Fatalf("flare wording %q leaked into step %q", b, g.Step)
			}
		}
	}
}

func TestManualGuidanceLinksComeFromCatalog(t *testing.T) {
	rows, err := loadAffiliateCatalog()
	if err != nil {
		t.Fatal(err)
	}
	known := make(map[string]string, len(rows))
	for _, r := range rows {
		known[r.ID] = r.AffiliateLink
	}

	guidance, picks := BuildManualProductGuidance(
		"barrier", "sensitive", []string{"redness", "dryness"}, "vi",
	)
	ctas := 0
	for _, g := range guidance {
		if g.AffiliateLink == "" {
			continue
		}
		ctas++
		want, ok := known[g.AffiliateProductID]
		if !ok {
			t.Fatalf("unknown catalog id %q", g.AffiliateProductID)
		}
		if g.AffiliateLink != want {
			t.Fatalf("link rewritten: got %q want %q", g.AffiliateLink, want)
		}
	}
	if ctas == 0 {
		t.Fatal("expected at least one affiliate CTA for a matchable profile")
	}
	if ctas > maxProductSuggestions {
		t.Fatalf("CTA budget exceeded: %d", ctas)
	}
	if len(picks) != ctas {
		t.Fatalf("picks (%d) must mirror guidance CTAs (%d)", len(picks), ctas)
	}
	for _, p := range picks {
		if strings.TrimSpace(p.Reason) == "" {
			t.Fatalf("suggestion without a reason: %+v", p)
		}
	}
}

func TestMergeGoalConcernsDedupes(t *testing.T) {
	got := mergeGoalConcerns("clear_acne", []string{"acne", "Acne", "large_pores"})
	seen := map[string]int{}
	for _, c := range got {
		seen[c]++
	}
	for c, n := range seen {
		if n > 1 {
			t.Fatalf("duplicate concern %q x%d", c, n)
		}
	}
	if seen["clogged_pores"] == 0 {
		t.Fatalf("goal needles not merged in: %v", got)
	}
}
