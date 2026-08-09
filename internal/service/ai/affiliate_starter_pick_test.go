package ai

import (
	"encoding/json"
	"strings"
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

func TestStripAffiliateFromOnboardingSnapshot_ClearsCommerce(t *testing.T) {
	snap, _ := json.Marshal(map[string]any{
		"locale": "en",
		"starter_routine": map[string]any{
			"morning": []string{"Cleanse"},
			"product_suggestions": []map[string]any{{
				"product_name": "Foaming", "brand": "CeraVe",
				"affiliate_link": "https://shopee.vn/x", "reason": "test",
			}},
			"product_guidance": []map[string]any{{
				"step": "cleanse", "category": "cleanser",
				"name_or_category": "CeraVe · Foaming",
				"brand": "CeraVe", "product_name": "Foaming",
				"affiliate_link": "https://shopee.vn/x",
				"affiliate_product_id": "x",
				"why": "CeraVe fits oily skin",
			}},
		},
		"skin_analysis": map[string]any{
			"product_suggestions": []map[string]any{{
				"product_name": "Foaming", "brand": "CeraVe",
				"affiliate_link": "https://shopee.vn/x", "reason": "test",
			}},
		},
	})
	out := StripAffiliateFromOnboardingSnapshot(snap, "en")
	var parsed struct {
		StarterRoutine struct {
			ProductSuggestions []any `json:"product_suggestions"`
			ProductGuidance    []struct {
				Brand         string `json:"brand"`
				AffiliateLink string `json:"affiliate_link"`
				Why           string `json:"why"`
			} `json:"product_guidance"`
		} `json:"starter_routine"`
		SkinAnalysis struct {
			ProductSuggestions []any `json:"product_suggestions"`
		} `json:"skin_analysis"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.StarterRoutine.ProductSuggestions) != 0 {
		t.Fatalf("starter suggestions should clear, got %d", len(parsed.StarterRoutine.ProductSuggestions))
	}
	if len(parsed.SkinAnalysis.ProductSuggestions) != 0 {
		t.Fatalf("analysis suggestions should clear, got %d", len(parsed.SkinAnalysis.ProductSuggestions))
	}
	if len(parsed.StarterRoutine.ProductGuidance) == 0 {
		t.Fatal("guidance roles should remain")
	}
	g := parsed.StarterRoutine.ProductGuidance[0]
	if g.Brand != "" || g.AffiliateLink != "" {
		t.Fatalf("commerce fields must clear: %+v", g)
	}
	if strings.Contains(strings.ToLower(g.Why), "cerave") {
		t.Fatalf("why still branded: %q", g.Why)
	}
}
