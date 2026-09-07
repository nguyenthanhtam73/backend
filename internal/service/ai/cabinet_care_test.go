package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestCareRoleFromText(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Rửa mặt dịu":      "cleanser",
		"cleanse":          "cleanser",
		"Dưỡng ẩm":         "moisturizer",
		"Chống nắng":       "spf",
		"kem chống nắng":   "spf",
		"spf":              "spf",
		"Serum":            "serum",
		"Giảm active mạnh": "",
	}
	for in, want := range cases {
		if got := CareRoleFromText(in); got != want {
			t.Fatalf("%q → %q want %q", in, got, want)
		}
	}
}

func TestApplyCabinetFirstCare_PrefersOwnedAndClearsCoveredAffiliate(t *testing.T) {
	t.Parallel()
	rows, _ := loadAffiliateCatalog()
	spf := findCatalogByCategory(rows, "spf")
	if spf == nil {
		t.Fatal("no spf in catalog")
	}
	ctx := `USER_MEMORY:
## Wardrobe (products user already owns — prefer these in care_suggestions; do NOT re-sell them)
- Sữa rửa mặt tạo bọt | brand: CeraVe | category: cleanser
- Kem dưỡng ẩm Hydro Boost Water Gel | brand: Neutrogena | category: moisturizer
- Kem chống nắng Relief Sun SPF50+ | brand: Beauty of Joseon | category: spf
`
	out := &CoachStructuredOutput{
		CareSuggestions: []CoachCareSuggestion{
			{Slot: "morning", Step: "Rửa mặt dịu", Why: "Má đang đỏ."},
			{Slot: "morning", Step: "Dưỡng ẩm", Why: "Giữ ẩm."},
			{Slot: "morning", Step: "Chống nắng", Why: "Hạn chế nắng."},
		},
		ProductSuggestions: []dto.ProductSuggestion{
			{ProductName: spf.ProductName, Brand: spf.Brand, Reason: "test", AffiliateLink: spf.AffiliateLink},
		},
		CarePhase: "calm_first",
	}
	ApplyCabinetFirstCare(out, ctx)
	if len(out.ProductSuggestions) != 0 {
		t.Fatalf("shelf covers today — want empty affiliate, got %d", len(out.ProductSuggestions))
	}
	if len(out.ProductGuidance) != 0 {
		t.Fatalf("guidance should clear with affiliate, got %d", len(out.ProductGuidance))
	}
	joined := out.CareSuggestions[0].Why + out.CareSuggestions[1].Why + out.CareSuggestions[2].Why
	if !strings.Contains(joined, "trong tủ") {
		t.Fatalf("care should name owned products, got %#v", out.CareSuggestions)
	}
	if !strings.Contains(out.CareSuggestions[1].Why, "Hydro Boost") {
		t.Fatalf("moisturizer why=%q", out.CareSuggestions[1].Why)
	}
}

func TestApplyCabinetFirstCare_KeepsGapAffiliate(t *testing.T) {
	t.Parallel()
	rows, _ := loadAffiliateCatalog()
	spf := findCatalogByCategory(rows, "spf")
	if spf == nil {
		t.Fatal("no spf")
	}
	ctx := `## Wardrobe (products user already owns)
- Sữa rửa mặt dưỡng ẩm | brand: CeraVe | category: cleanser
- Kem dưỡng ẩm Hydro Boost Water Gel | brand: Neutrogena | category: moisturizer
`
	out := &CoachStructuredOutput{
		CareSuggestions: []CoachCareSuggestion{
			{Slot: "morning", Step: "Rửa mặt dịu", Why: "Sạch nhẹ."},
			{Slot: "morning", Step: "Chống nắng", Why: "Ra nắng hôm qua."},
		},
		ProductSuggestions: []dto.ProductSuggestion{
			{ProductName: spf.ProductName, Brand: spf.Brand, Reason: "quên SPF", AffiliateLink: spf.AffiliateLink, Step: "spf"},
		},
	}
	ApplyCabinetFirstCare(out, ctx)
	if len(out.ProductSuggestions) != 1 {
		t.Fatalf("missing SPF should stay, got %d", len(out.ProductSuggestions))
	}
}

func TestDropCoveredRoleProductSuggestions(t *testing.T) {
	t.Parallel()
	rows, _ := loadAffiliateCatalog()
	spf := findCatalogByCategory(rows, "spf")
	if spf == nil {
		t.Fatal("no spf")
	}
	owned := []wardrobeItem{{Name: "Relief Sun", Brand: "BoJ", Category: "spf"}}
	got := DropCoveredRoleProductSuggestions([]dto.ProductSuggestion{
		{ProductName: spf.ProductName, Brand: spf.Brand, AffiliateLink: spf.AffiliateLink},
	}, owned)
	if len(got) != 0 {
		t.Fatalf("covered SPF role should drop, got %d", len(got))
	}
}
