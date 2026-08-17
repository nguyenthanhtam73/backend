package routine

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
)

func TestRoutineToday_MatchesStreaktime(t *testing.T) {
	t.Parallel()
	got := routineToday()
	want := streaktime.Today()
	if !got.Equal(want) {
		t.Fatalf("routineToday=%v want streaktime.Today()=%v", got, want)
	}
}

func TestParseRoutineDate_EmptyDefaultsToRoutineToday(t *testing.T) {
	t.Parallel()
	got, err := parseRoutineDate("")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(routineToday()) {
		t.Fatalf("parseRoutineDate(\"\")=%v want routineToday()=%v", got, routineToday())
	}
}

func TestParseRoutineDate_ExplicitISO(t *testing.T) {
	t.Parallel()
	got, err := parseRoutineDate("2026-07-16")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestRoutineToday_AlignedWithVietnamCalendarBoundary(t *testing.T) {
	t.Parallel()
	// 2026-07-15 17:30 UTC == 2026-07-16 00:30 VN — routine "today" must be the 16th.
	vnDay := streaktime.DateOf(time.Date(2026, 7, 15, 17, 30, 0, 0, time.UTC))
	if vnDay.Format("2006-01-02") != "2026-07-16" {
		t.Fatalf("VN calendar day=%s want 2026-07-16", vnDay.Format("2006-01-02"))
	}
	// routineToday() delegates to the same streaktime.Today(); boundary is covered
	// in streaktime_test — this asserts the helper we call is wired correctly.
	if !routineToday().Equal(streaktime.Today()) {
		t.Fatal("routineToday must stay aligned with streaktime.Today()")
	}
}
