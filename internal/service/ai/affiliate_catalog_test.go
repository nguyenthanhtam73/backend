package ai

import (
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestSanitizeProductSuggestions_KeepsValidCatalogMatch(t *testing.T) {
	rows, err := loadAffiliateCatalog()
	if err != nil || len(rows) == 0 {
		t.Fatalf("catalog: %v len=%d", err, len(rows))
	}
	entry := rows[0]

	out := SanitizeProductSuggestions([]dto.ProductSuggestion{
		{
			ProductName:   entry.ProductName,
			Brand:         entry.Brand,
			Reason:        "Phù hợp vì da bạn hay bóng dầu vùng chữ T.",
			AffiliateLink: entry.AffiliateLink,
			PriceRange:    "999k",
			Priority:      "high",
		},
	})
	if len(out) != 1 {
		t.Fatalf("want 1 suggestion, got %d", len(out))
	}
	if out[0].AffiliateLink != entry.AffiliateLink {
		t.Fatalf("link should come from catalog: got %q want %q", out[0].AffiliateLink, entry.AffiliateLink)
	}
	if out[0].PriceRange != entry.PriceRange {
		t.Fatalf("price should come from catalog: got %q want %q", out[0].PriceRange, entry.PriceRange)
	}
	if out[0].Priority != "high" {
		t.Fatalf("priority=%q", out[0].Priority)
	}
}

func TestSanitizeProductSuggestions_DropsUnknown(t *testing.T) {
	out := SanitizeProductSuggestions([]dto.ProductSuggestion{
		{
			ProductName:   "Fake Product XYZ",
			Brand:         "Unknown Brand",
			Reason:        "Should be dropped",
			AffiliateLink: "https://evil.example/phish",
			PriceRange:    "100k",
			Priority:      "high",
		},
	})
	if len(out) != 0 {
		t.Fatalf("want empty, got %v", out)
	}
}

func TestSanitizeProductSuggestions_CapsAtTwo(t *testing.T) {
	rows, err := loadAffiliateCatalog()
	if err != nil || len(rows) < 4 {
		t.Fatalf("need catalog rows: %v len=%d", err, len(rows))
	}
	raw := make([]dto.ProductSuggestion, 0, 5)
	for i := 0; i < 5 && i < len(rows); i++ {
		raw = append(raw, dto.ProductSuggestion{
			ProductName:   rows[i].ProductName,
			Brand:         rows[i].Brand,
			Reason:        "Lý do cụ thể cho sản phẩm này.",
			AffiliateLink: rows[i].AffiliateLink,
			Priority:      "medium",
		})
	}
	out := SanitizeProductSuggestions(raw)
	if len(out) != maxProductSuggestions {
		t.Fatalf("want %d suggestions, got %d", maxProductSuggestions, len(out))
	}
}
