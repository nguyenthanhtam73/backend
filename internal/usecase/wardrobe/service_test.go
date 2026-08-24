package wardrobe

import (
	"testing"
)

func TestNormalizeProductFields_NameRequiredBrandOptional(t *testing.T) {
	_, _, _, _, _, err := normalizeProductFields("", "CeraVe", "", "", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}

	name, brand, _, _, _, err := normalizeProductFields("Cleanser", "", "", "", "")
	if err != nil {
		t.Fatalf("empty brand should be allowed: %v", err)
	}
	if name != "Cleanser" || brand != "" {
		t.Fatalf("got name=%q brand=%q", name, brand)
	}

	_, _, _, _, _, err = normalizeProductFields("   ", "CeraVe", "", "", "")
	if err == nil {
		t.Fatal("expected error for whitespace-only name")
	}
}

func TestParseOpenedAt(t *testing.T) {
	got, err := parseOpenedAt("2026-06-02")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got == nil {
		t.Fatal("expected date")
	}
	if got.Format("2006-01-02") != "2026-06-02" {
		t.Fatalf("got %s", got.Format("2006-01-02"))
	}
	if _, err := parseOpenedAt("not-a-date"); err == nil {
		t.Fatal("expected error for invalid date")
	}
}
