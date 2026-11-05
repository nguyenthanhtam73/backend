package routine

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
)

func TestRoutineToday_IsCareDay(t *testing.T) {
	t.Parallel()
	if !routineToday().Equal(routineCareDay(time.Now())) {
		t.Fatal("routineToday must follow routineCareDay")
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

func TestRoutineCareDay_EveningBefore0400StaysOnPreviousDay(t *testing.T) {
	t.Parallel()
	loc := streaktime.Location
	vn := func(y int, m time.Month, d, hour, min int) time.Time {
		return time.Date(y, m, d, hour, min, 0, 0, loc)
	}
	cases := []struct {
		at   time.Time
		want string
	}{
		{vn(2026, 11, 5, 0, 0), "2026-11-04"},
		{vn(2026, 11, 5, 3, 59), "2026-11-04"},
		{vn(2026, 11, 5, 4, 0), "2026-11-05"},
		{vn(2026, 11, 5, 16, 0), "2026-11-05"},
		{vn(2026, 11, 5, 23, 30), "2026-11-05"},
	}
	for _, tc := range cases {
		if got := routineCareDay(tc.at).Format("2006-01-02"); got != tc.want {
			t.Fatalf("%s care day %s want %s", tc.at.In(loc).Format(time.RFC3339), got, tc.want)
		}
	}

	// 00:30 Vietnam is already the next check-in day. The evening routine is not.
	justAfterMidnight := vn(2026, 7, 16, 0, 30)
	if streaktime.DateOf(justAfterMidnight).Format("2006-01-02") != "2026-07-16" {
		t.Fatal("check-in day must stay on the civil midnight")
	}
	if routineCareDay(justAfterMidnight).Format("2006-01-02") != "2026-07-15" {
		t.Fatal("00:30 evening tick must stay on the previous routine day")
	}
}
