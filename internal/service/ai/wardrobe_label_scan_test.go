package ai

import "testing"

func TestNormalizeWardrobeLabelScan(t *testing.T) {
	got := normalizeWardrobeLabelScan(wardrobeLabelScanRaw{
		Name:       "  Hydrating Cleanser ",
		Brand:      " CeraVe ",
		Category:   "CLEANSER",
		Notes:      " AM/PM ",
		Confidence: 1.4,
	})
	if got.Name != "Hydrating Cleanser" || got.Brand != "CeraVe" || got.Category != "cleanser" {
		t.Fatalf("got %+v", got)
	}
	if got.Confidence != 1 {
		t.Fatalf("confidence clamp: %v", got.Confidence)
	}
	if got.Notes != "AM/PM" {
		t.Fatalf("notes: %q", got.Notes)
	}

	unknown := normalizeWardrobeLabelScan(wardrobeLabelScanRaw{Category: "face wash", Confidence: -1})
	if unknown.Category != "other" || unknown.Confidence != 0 {
		t.Fatalf("unknown: %+v", unknown)
	}
}
