package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestGuidanceAffiliate_MatchesFirstGuidancePickForSkin(t *testing.T) {
	t.Parallel()
	for _, skin := range []string{"oily", "dry", "normal", ""} {
		for _, tc := range []struct {
			category string
			title    string
		}{
			{category: "cleanser", title: "Rửa mặt dịu"},
			{category: "spf", title: "Kem chống nắng"},
			{category: "moisturizer", title: "Kem dưỡng ẩm"},
			{category: "", title: "Kem chống nắng"},
		} {
			gotID, gotName, gotLink, gotOK := GuidanceAffiliateForRoutineStep(skin, tc.category, tc.title)
			wantID, wantName, wantLink, wantOK := firstGuidanceProductFromBuilder(skin, tc.category, tc.title)
			if gotOK != wantOK || gotID != wantID || gotName != wantName || gotLink != wantLink {
				t.Fatalf("skin %q %s %q: got (%s,%s,%s,%v) want (%s,%s,%s,%v)",
					skin, tc.category, tc.title, gotID, gotName, gotLink, gotOK, wantID, wantName, wantLink, wantOK)
			}
			if gotOK && !strings.HasPrefix(gotLink, "https://") {
				t.Fatalf("non-https link %s", gotLink)
			}
		}
	}
}

func TestGuidanceAffiliate_SkinTypeChangesCleanserPick(t *testing.T) {
	t.Parallel()
	oily, _, _, okOily := GuidanceAffiliateForRoutineStep("oily", "cleanser", "Rửa mặt")
	dry, _, _, okDry := GuidanceAffiliateForRoutineStep("dry", "cleanser", "Rửa mặt")
	if !okOily || !okDry {
		t.Fatal("expected a guidance cleanser for oily and dry")
	}
	if oily == dry {
		t.Fatal("oily and dry skin resolved to the same cleanser; skin type was ignored")
	}
}

func TestGuidanceAffiliate_NoCardStaysEmpty(t *testing.T) {
	t.Parallel()
	cases := []struct{ category, title string }{
		{category: "mask", title: "Mặt nạ"},
		{category: "eye", title: "Kem mắt"},
		{category: "serum", title: "Serum"},
		{category: "toner", title: "Toner"},
		{category: "treatment", title: "Điều trị"},
		{category: "other", title: "Uống nước"},
	}
	for _, tc := range cases {
		id, name, link, ok := GuidanceAffiliateForRoutineStep("oily", tc.category, tc.title)
		if ok || id != "" || name != "" || link != "" {
			t.Fatalf("%s/%s matched %s %s %s", tc.category, tc.title, id, name, link)
		}
	}
}

func TestAttachForSkin_UniqueCatalogMatchWinsOverGuidance(t *testing.T) {
	t.Parallel()
	guidanceID, _, _, ok := GuidanceAffiliateForRoutineStep("oily", "cleanser", "Rửa mặt")
	if !ok {
		t.Fatal("expected guidance cleanser")
	}
	rows, err := loadAffiliateCatalog()
	if err != nil {
		t.Fatal(err)
	}
	var other affiliateCatalogEntry
	found := false
	for _, r := range rows {
		if r.Category == "cleanser" && r.ID != guidanceID {
			other = r
			found = true
			break
		}
	}
	if !found {
		t.Fatal("catalog needs two cleansers to prove unique match wins")
	}
	out := AttachRoutineStepAffiliatesForSkin([]dto.RoutineStep{
		{ID: "named", Title: other.ProductName, Category: "cleanser"},
		{ID: "generic", Title: "Rửa mặt dịu", Category: "cleanser"},
		{ID: "mask", Title: "Mặt nạ", Category: "mask"},
	}, "oily")
	if out[0].AffiliateProductID == nil || *out[0].AffiliateProductID != other.ID {
		t.Fatalf("unique name should keep catalog id %s, got %#v", other.ID, out[0].AffiliateProductID)
	}
	if out[0].AffiliateLink == nil || *out[0].AffiliateLink != other.AffiliateLink {
		t.Fatalf("unique link %#v", out[0].AffiliateLink)
	}
	if out[1].AffiliateProductID == nil || *out[1].AffiliateProductID != guidanceID {
		t.Fatalf("generic step got %#v want guidance %s", out[1].AffiliateProductID, guidanceID)
	}
	if out[2].AffiliateLink != nil || out[2].AffiliateProductID != nil || out[2].ProductName != nil {
		t.Fatalf("mask should stay null: %#v", out[2])
	}
}

func firstGuidanceProductFromBuilder(skin, category, title string) (string, string, string, bool) {
	items, _ := BuildOnboardingProductGuidance(PhaseCalmFirst, "", skin, nil, nil, "")
	return firstGuidanceAffiliate(items, category, title)
}
