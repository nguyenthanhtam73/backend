package checkinreminder

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
)

func vn(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, streaktime.Location)
}

func TestSelect_D0DueOnSignupDayWithoutCheckIn(t *testing.T) {
	got := Select(Input{
		SignupAt:       vn(2026, 9, 5, 9, 15),
		Now:            vn(2026, 9, 5, 18, 0),
		CheckedInToday: false,
		AccountActive:  true,
	})
	if got.Kind != KindD0 || !got.Due || got.DaysSinceSignup != 0 {
		t.Fatalf("got %+v", got)
	}
	if got.SignupDate != "2026-09-05" {
		t.Fatalf("signup_date=%s", got.SignupDate)
	}
}

func TestSelect_D0NotDueAfterCheckIn(t *testing.T) {
	got := Select(Input{
		SignupAt:       vn(2026, 9, 5, 9, 15),
		Now:            vn(2026, 9, 5, 18, 0),
		CheckedInToday: true,
		AccountActive:  true,
	})
	if got.Kind != KindD0 || got.Due {
		t.Fatalf("checked-in D0 should stay kind=d0 due=false: %+v", got)
	}
}

func TestSelect_D1DueNextCivilDay(t *testing.T) {
	got := Select(Input{
		SignupAt:       vn(2026, 9, 5, 22, 0),
		Now:            vn(2026, 9, 6, 8, 0),
		CheckedInToday: false,
		AccountActive:  true,
	})
	if got.Kind != KindD1 || !got.Due || got.DaysSinceSignup != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestSelect_D1NotDueAfterCheckIn(t *testing.T) {
	got := Select(Input{
		SignupAt:       vn(2026, 9, 5, 10, 0),
		Now:            vn(2026, 9, 6, 20, 0),
		CheckedInToday: true,
		AccountActive:  true,
	})
	if got.Kind != KindD1 || got.Due {
		t.Fatalf("got %+v", got)
	}
}

func TestSelect_PastWindowIsNone(t *testing.T) {
	got := Select(Input{
		SignupAt:       vn(2026, 9, 1, 10, 0),
		Now:            vn(2026, 9, 5, 10, 0),
		CheckedInToday: false,
		AccountActive:  true,
	})
	if got.Kind != KindNone || got.Due || got.DaysSinceSignup != 4 {
		t.Fatalf("got %+v", got)
	}
}

func TestSelect_InactiveAccountNeverDue(t *testing.T) {
	got := Select(Input{
		SignupAt:       vn(2026, 9, 5, 10, 0),
		Now:            vn(2026, 9, 5, 12, 0),
		CheckedInToday: false,
		AccountActive:  false,
	})
	if got.Kind != KindD0 || got.Due {
		t.Fatalf("inactive should not be due: %+v", got)
	}
}

func TestSelect_VietnamMidnightBoundary(t *testing.T) {
	// 23:30 VN on Sep 4 is still D0. 00:30 VN on Sep 5 is D1.
	// Those instants are 16:30 and 17:30 UTC — easy to get wrong if using UTC days.
	signup := vn(2026, 9, 4, 23, 30)
	justBeforeMidnight := vn(2026, 9, 4, 23, 59)
	justAfterMidnight := vn(2026, 9, 5, 0, 30)

	d0 := Select(Input{SignupAt: signup, Now: justBeforeMidnight, AccountActive: true})
	if d0.Kind != KindD0 || !d0.Due {
		t.Fatalf("23:59 VN should be D0: %+v", d0)
	}
	d1 := Select(Input{SignupAt: signup, Now: justAfterMidnight, AccountActive: true})
	if d1.Kind != KindD1 || !d1.Due {
		t.Fatalf("00:30 VN should be D1: %+v", d1)
	}
}

func TestSelect_ZeroSignupIsNone(t *testing.T) {
	got := Select(Input{Now: vn(2026, 9, 5, 12, 0), AccountActive: true})
	if got.Kind != KindNone || got.Due {
		t.Fatalf("got %+v", got)
	}
}

func TestSignupWindow_CoversYesterdayAndTodayVN(t *testing.T) {
	now := vn(2026, 9, 5, 15, 0)
	from, to := SignupWindow(now)
	if !from.Equal(vn(2026, 9, 4, 0, 0)) {
		t.Fatalf("from=%s", from)
	}
	if !to.Equal(vn(2026, 9, 6, 0, 0)) {
		t.Fatalf("to=%s", to)
	}
}

func TestNormalizeKind(t *testing.T) {
	if NormalizeKind("D0") != KindD0 || NormalizeKind("d1") != KindD1 || NormalizeKind("x") != KindNone {
		t.Fatal("normalize")
	}
}
