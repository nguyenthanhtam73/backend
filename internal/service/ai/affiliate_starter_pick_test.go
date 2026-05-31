package ai

import (
	"encoding/json"
	"testing"
)

func TestPickStarterAffiliateSuggestions_OutdoorSPF(t *testing.T) {
	snap, _ := json.Marshal(map[string]any{
		"skin_type": "oily",
		"goal":      "clear_acne",
		"contexts":  []string{"outdoor"},
		"locale":    "vi",
	})
	out := PickStarterAffiliateSuggestions(snap, "vi")
	if len(out) != 1 {
		t.Fatalf("want 1 pick, got %d", len(out))
	}
	if out[0].AffiliateLink == "" {
		t.Fatal("expected affiliate link")
	}
	rows, _ := loadAffiliateCatalog()
	var entry *affiliateCatalogEntry
	for i := range rows {
		if rows[i].AffiliateLink == out[0].AffiliateLink {
			entry = &rows[i]
			break
		}
	}
	if entry == nil || entry.Category != "spf" {
		t.Fatalf("expected spf pick, got %+v", out[0])
	}
}

func TestEnrichOnboardingSnapshotStarterAffiliate_InjectsWhenMissing(t *testing.T) {
	snap, _ := json.Marshal(map[string]any{
		"skin_type": "dry",
		"goal":      "barrier",
		"locale":    "vi",
		"starter_routine": map[string]any{
			"morning":  []string{"Rửa mặt"},
			"evening":  []string{},
			"week_notes": "test",
		},
	})
	enriched := EnrichOnboardingSnapshotStarterAffiliate(snap, "vi", nil)
	var parsed struct {
		StarterRoutine struct {
			ProductSuggestions []struct {
				AffiliateLink string `json:"affiliate_link"`
			} `json:"product_suggestions"`
		} `json:"starter_routine"`
	}
	if err := json.Unmarshal(enriched, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.StarterRoutine.ProductSuggestions) != 1 {
		t.Fatalf("want 1 injected suggestion, got %d", len(parsed.StarterRoutine.ProductSuggestions))
	}
}
