package dto

import (
	"testing"
	"time"
)

func TestFormatUserMemorySummary(t *testing.T) {
	now := time.Date(2026, 10, 23, 15, 0, 0, 0, time.FixedZone("ICT", 7*3600))
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if got := FormatUserMemorySummary(day, 1, now); got != "Ngày 22/10 · 1 ảnh" {
		t.Fatalf("one photo: %q", got)
	}
	if got := FormatUserMemorySummary(day, 0, now); got != "Ngày 22/10 · không có ảnh" {
		t.Fatalf("no photo: %q", got)
	}
	if got := FormatUserMemorySummary(day, 2, now); got != "Ngày 22/10 · 2 ảnh" {
		t.Fatalf("two photos: %q", got)
	}
	older := time.Date(2025, 10, 22, 0, 0, 0, 0, time.UTC)
	if got := FormatUserMemorySummary(older, 1, now); got != "Ngày 22/10/2025 · 1 ảnh" {
		t.Fatalf("older year: %q", got)
	}
}
