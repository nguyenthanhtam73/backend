package checkinreminder

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
)

func vnAt(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, streaktime.Location)
}

func TestSelectEveningEmail_D1DayAfterFirstCheckIn(t *testing.T) {
	first := vnAt(2026, 10, 4, 21, 0)
	// 00:30 the next morning is the right civil day but the email must not be
	// treated as due-for-send by the selector's clock — eligibility is the day,
	// the job gates 19:30. The selector itself is day-based.
	got := SelectEveningEmail(EveningInput{
		FirstCheckDate: first,
		Now:            vnAt(2026, 10, 5, 19, 30),
		AccountActive:  true,
	})
	if got.Kind != KindD1 || !got.Due || got.DaysSinceFirstCheck != 1 {
		t.Fatalf("d1: %+v", got)
	}
	checked := SelectEveningEmail(EveningInput{
		FirstCheckDate: first,
		Now:            vnAt(2026, 10, 5, 19, 30),
		CheckedInToday: true,
		AccountActive:  true,
	})
	if checked.Due {
		t.Fatalf("checked in today should not be due: %+v", checked)
	}
	// Signup day is irrelevant: a first check-in yesterday is D1 even if the
	// account is older than one day. (The caller passes first-check, not signup.)
	older := SelectEveningEmail(EveningInput{
		FirstCheckDate: vnAt(2026, 10, 4, 8, 0),
		Now:            vnAt(2026, 10, 5, 0, 30),
		AccountActive:  true,
	})
	if older.Kind != KindD1 || !older.Due {
		t.Fatalf("civil day after first check-in is D1 at 00:30 too: %+v", older)
	}
}

func TestSelectEveningEmail_D3OnlyIfMissedDay2(t *testing.T) {
	first := vnAt(2026, 10, 2, 18, 0) // F
	// F+3 = Oct 5. Day 2 = Oct 4.
	base := EveningInput{
		FirstCheckDate: first,
		Now:            vnAt(2026, 10, 5, 19, 30),
		AccountActive:  true,
	}
	missed := SelectEveningEmail(base)
	if missed.Kind != KindD3 || !missed.Due || missed.DaysSinceFirstCheck != 3 {
		t.Fatalf("missed day 2: %+v", missed)
	}
	returned := base
	returned.CheckedInOnDay2 = true
	got := SelectEveningEmail(returned)
	if got.Kind != KindNone || got.Due {
		t.Fatalf("returned on day 2: %+v", got)
	}
	today := base
	today.CheckedInToday = true
	if SelectEveningEmail(today).Due {
		t.Fatal("day-3 with a check-in today must not be due")
	}
	inactive := base
	inactive.AccountActive = false
	if SelectEveningEmail(inactive).Due {
		t.Fatal("inactive account must not be due")
	}
}

func TestSelectEveningEmail_NoEmailOnOtherDays(t *testing.T) {
	first := vnAt(2026, 10, 1, 9, 0)
	cases := []struct {
		name string
		now  time.Time
	}{
		{"same day", vnAt(2026, 10, 1, 19, 30)},
		{"day 2", vnAt(2026, 10, 3, 19, 30)},
		{"day 4", vnAt(2026, 10, 5, 19, 30)},
	}
	for _, tc := range cases {
		got := SelectEveningEmail(EveningInput{
			FirstCheckDate: first,
			Now:            tc.now,
			AccountActive:  true,
		})
		if got.Due || got.Kind != KindNone {
			t.Fatalf("%s: %+v", tc.name, got)
		}
	}
}
