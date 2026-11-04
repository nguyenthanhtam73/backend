package roomcare

import (
	"testing"
	"time"
)

func TestCareStreak(t *testing.T) {
	today := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	day := func(back int) time.Time { return today.AddDate(0, 0, -back) }

	if got := CareStreak(today, nil); got != 0 {
		t.Fatalf("none %d", got)
	}
	if got := CareStreak(today, []time.Time{today}); got != 1 {
		t.Fatalf("today only %d", got)
	}
	if got := CareStreak(today, []time.Time{today, day(1), day(2)}); got != 3 {
		t.Fatalf("through today %d", got)
	}
	if got := CareStreak(today, []time.Time{day(1), day(2)}); got != 2 {
		t.Fatalf("through yesterday %d", got)
	}
	if got := CareStreak(today, []time.Time{day(2), day(3)}); got != 0 {
		t.Fatalf("gap yesterday %d", got)
	}
	if got := CareStreak(today, []time.Time{today, day(2)}); got != 1 {
		t.Fatalf("gap before today %d", got)
	}
	// A later instant on the same Vietnam day is still that civil day.
	evening := today.Add(16 * time.Hour)
	if got := CareStreak(evening, []time.Time{today, day(1)}); got != 2 {
		t.Fatalf("same civil day %d", got)
	}
}
