package main

import (
	"os"
	"testing"
)

func TestLoadCookieHeader_SemicolonLine(t *testing.T) {
	path := t.TempDir() + "/cookies.txt"
	if err := osWrite(path, "SPC_F=abc; csrftoken=xyz"); err != nil {
		t.Fatal(err)
	}
	got, err := loadCookieHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "SPC_F=abc; csrftoken=xyz" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadCookieHeader_JSON(t *testing.T) {
	path := t.TempDir() + "/cookies.json"
	if err := osWrite(path, `[{"name":"SPC_F","value":"abc"},{"name":"csrftoken","value":"xyz"}]`); err != nil {
		t.Fatal(err)
	}
	got, err := loadCookieHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "SPC_F=abc; csrftoken=xyz" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatPriceRange(t *testing.T) {
	if got := formatPriceRange(43900000, 43900000); got != "439k" {
		t.Fatalf("got %q", got)
	}
	if got := formatPriceRange(25000000, 32000000); got != "250k-320k" {
		t.Fatalf("got %q", got)
	}
}

func TestParseShopItemIDs(t *testing.T) {
	s, i := parseShopItemIDs("https://shopee.vn/product/554005/27560289520?x=1")
	if s != "554005" || i != "27560289520" {
		t.Fatalf("product path: %s %s", s, i)
	}
	s, i = parseShopItemIDs("https://shopee.vn/foo-i.812449960.18832327227")
	if s != "812449960" || i != "18832327227" {
		t.Fatalf("i. path: %s %s", s, i)
	}
}

func osWrite(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
