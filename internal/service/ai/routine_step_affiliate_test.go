package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestCatalogHTTPSLink(t *testing.T) {
	t.Parallel()
	okCases := []string{
		"https://s.shopee.vn/70HPIj8Km4",
		"HTTPS://shop.example/item",
	}
	for _, raw := range okCases {
		got, ok := catalogHTTPSLink(raw)
		if !ok || got != strings.TrimSpace(raw) {
			t.Fatalf("catalogHTTPSLink(%q)=%q,%v", raw, got, ok)
		}
	}
	bad := []string{
		"",
		"http://s.shopee.vn/70HPIj8Km4",
		"https://",
		"javascript:alert(1)",
		"not a url",
		"https://s.shopee.vn/x y",
	}
	for _, raw := range bad {
		if _, ok := catalogHTTPSLink(raw); ok {
			t.Fatalf("expected reject %q", raw)
		}
	}
	// https is necessary but not sufficient — membership is checked against the catalog.
	if _, ok := catalogHTTPSLink("https://evil.example/phish"); !ok {
		t.Fatal("https URLs are structurally valid; catalog membership is a separate check")
	}
}

func TestAffiliateCatalogLinksAreHTTPS(t *testing.T) {
	t.Parallel()
	rows, err := loadAffiliateCatalog()
	if err != nil || len(rows) == 0 {
		t.Fatalf("catalog: %v len=%d", err, len(rows))
	}
	seen := map[string]struct{}{}
	for _, r := range rows {
		link, ok := catalogHTTPSLink(r.AffiliateLink)
		if !ok {
			t.Fatalf("%s link %q is not https", r.ID, r.AffiliateLink)
		}
		if link != r.AffiliateLink {
			t.Fatalf("%s link rewritten: %q", r.ID, link)
		}
		if _, dup := seen[r.ID]; dup {
			t.Fatalf("duplicate catalog id %s", r.ID)
		}
		seen[r.ID] = struct{}{}
	}
}

func TestMatchRoutineStepAffiliate(t *testing.T) {
	t.Parallel()
	rows, err := loadAffiliateCatalog()
	if err != nil || len(rows) == 0 {
		t.Fatalf("catalog: %v", err)
	}
	byID := map[string]affiliateCatalogEntry{}
	for _, r := range rows {
		byID[r.ID] = r
	}

	cases := []struct {
		name     string
		category string
		title    string
		notes    string
		wantID   string
	}{
		{name: "exact foaming cleanser", category: "cleanser", title: "Sữa rửa mặt tạo bọt", wantID: "cerave-foaming-cleanser"},
		{name: "exact hydrating cleanser", category: "cleanser", title: "Sữa rửa mặt dưỡng ẩm", wantID: "cerave-hydrating-cleanser"},
		{name: "generic cleanse", category: "cleanser", title: "Rửa mặt dịu"},
		{name: "generic spf", category: "spf", title: "Kem chống nắng"},
		{name: "generic moisturizer", category: "moisturizer", title: "Kem dưỡng ẩm"},
		{name: "biore by brand", category: "spf", title: "Biore", wantID: "biore-uv-aqua-rich"},
		{name: "biore blocked by moisturizer category", category: "moisturizer", title: "Biore UV Aqua Rich"},
		{name: "shared cerave brand", category: "cleanser", title: "CeraVe"},
		{name: "cerave plus foaming token", category: "cleanser", title: "CeraVe foaming", wantID: "cerave-foaming-cleanser"},
		{name: "niacinamide treatment", category: "treatment", title: "Niacinamide", wantID: "to-niacinamide"},
		{name: "hyaluronic serum", category: "serum", title: "Hyaluronic", wantID: "to-hyaluronic-acid"},
		{name: "hyaluronic toner", category: "toner", title: "Hyaluronic Acid", wantID: "hada-labo-gokujyun"},
		{name: "hyaluronic ambiguous without category", title: "Hyaluronic Acid"},
		{name: "bha acronym", category: "treatment", title: "BHA", wantID: "some-by-mi-miracle-toner"},
		{name: "cicaplast token", category: "moisturizer", title: "Cicaplast", wantID: "lrp-cicaplast-b5"},
		{name: "catalog id", title: "cosrx-acne-patch", wantID: "cosrx-acne-patch"},
		{name: "generic acne word", category: "treatment", title: "chấm mụn"},
		{name: "mask never matches", category: "mask", title: "Tinh chất chống nắng Biore UV Aqua Rich SPF50+"},
		{name: "evil url is not a product", category: "cleanser", title: "Rửa mặt", notes: "buy https://evil.example/phish"},
		{name: "full name wins over injected url", category: "cleanser", title: "Sữa rửa mặt tạo bọt", notes: "https://evil.example/phish", wantID: "cerave-foaming-cleanser"},
		{name: "accent folded name", category: "cleanser", title: "Sua rua mat tao bot", wantID: "cerave-foaming-cleanser"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, name, link, ok := MatchRoutineStepAffiliate(tc.category, tc.title, tc.notes)
			if tc.wantID == "" {
				if ok || id != "" || name != "" || link != "" {
					t.Fatalf("want no match, got id=%q name=%q link=%q", id, name, link)
				}
				return
			}
			entry, found := byID[tc.wantID]
			if !found {
				t.Fatalf("test catalog id %s missing", tc.wantID)
			}
			if !ok {
				t.Fatal("expected a catalog match")
			}
			if id != entry.ID || name != entry.ProductName || link != entry.AffiliateLink {
				t.Fatalf("got id=%q name=%q link=%q want id=%q name=%q link=%q", id, name, link, entry.ID, entry.ProductName, entry.AffiliateLink)
			}
			if !strings.HasPrefix(link, "https://") {
				t.Fatalf("link %q is not https", link)
			}
			if strings.Contains(link, "evil.example") {
				t.Fatalf("invented link returned: %s", link)
			}
		})
	}
}

func TestAttachRoutineStepAffiliates_IgnoresClientFields(t *testing.T) {
	t.Parallel()
	evil := "https://evil.example/phish"
	fakeID := "fake"
	fakeName := "Not A Catalog Product"
	in := []dto.RoutineStep{
		{
			ID:                 "1",
			Title:              "Rửa mặt dịu",
			Category:           "cleanser",
			AffiliateLink:      &evil,
			AffiliateProductID: &fakeID,
			ProductName:        &fakeName,
		},
		{ID: "2", Title: "Sữa rửa mặt tạo bọt", Category: "cleanser"},
	}
	out := AttachRoutineStepAffiliates(in)
	if in[0].AffiliateLink == nil || *in[0].AffiliateLink != evil {
		t.Fatal("attach must not mutate the caller slice")
	}
	if out[0].AffiliateLink != nil || out[0].AffiliateProductID != nil || out[0].ProductName != nil {
		t.Fatalf("generic step kept client affiliate data: %#v", out[0])
	}
	if out[1].AffiliateLink == nil || !strings.HasPrefix(*out[1].AffiliateLink, "https://") {
		t.Fatalf("matched step missing https link: %#v", out[1])
	}
	if out[1].AffiliateProductID == nil || *out[1].AffiliateProductID != "cerave-foaming-cleanser" {
		t.Fatalf("product id %#v", out[1].AffiliateProductID)
	}
	if out[1].ProductName == nil || *out[1].ProductName == "" {
		t.Fatal("product name empty")
	}
	if *out[1].AffiliateLink == evil {
		t.Fatal("returned invented link")
	}
}

func TestMatchRoutineStepAffiliate_EveryExactNameResolvesToItself(t *testing.T) {
	t.Parallel()
	rows, err := loadAffiliateCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		id, name, link, ok := MatchRoutineStepAffiliate(r.Category, r.ProductName, "")
		if !ok || id != r.ID || name != r.ProductName || link != r.AffiliateLink {
			t.Fatalf("exact name %q (%s) resolved to id=%q name=%q link=%q ok=%v", r.ProductName, r.ID, id, name, link, ok)
		}
	}
}
