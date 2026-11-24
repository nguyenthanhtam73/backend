package streaktime

import (
	"testing"
	"time"
)

func TestDateOf_VietnamMidnightBoundary(t *testing.T) {
	// 2026-07-15 17:30 UTC == 2026-07-16 00:30 Asia/Ho_Chi_Minh
	justAfterVNMidnight := time.Date(2026, 7, 15, 17, 30, 0, 0, time.UTC)
	got := DateOf(justAfterVNMidnight)
	want := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("DateOf=%v want %v (VN calendar day)", got, want)
	}

	// 2026-07-15 16:59 UTC == 2026-07-15 23:59 VN — still previous VN day
	justBeforeVNMidnight := time.Date(2026, 7, 15, 16, 59, 0, 0, time.UTC)
	got = DateOf(justBeforeVNMidnight)
	want = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("DateOf=%v want %v", got, want)
	}
}

func TestDateOf_NotRawUTCTruncation(t *testing.T) {
	// Evening VN still same VN day; UTC truncation would already be "next" or "prev".
	eveningVNAsUTC := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC) // 03:00 next day VN
	got := DateOf(eveningVNAsUTC)
	want := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("DateOf=%v want %v", got, want)
	}
}

func TestRoutineCareDay_RollsAt0400(t *testing.T) {
	loc := Location
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
		if got := RoutineCareDay(tc.at).Format("2006-01-02"); got != tc.want {
			t.Fatalf("%s care day %s want %s", tc.at.In(loc).Format(time.RFC3339), got, tc.want)
		}
	}
	// 00:30 Vietnam is the next check-in day. The evening routine is not.
	justAfterMidnight := vn(2026, 11, 5, 0, 30)
	if DateOf(justAfterMidnight).Format("2006-01-02") != "2026-11-05" {
		t.Fatal("check-in day stays on the civil midnight")
	}
	if RoutineCareDay(justAfterMidnight).Format("2006-01-02") != "2026-11-04" {
		t.Fatal("00:30 still belongs to the previous routine day")
	}
}

func TestNow_AndTodayString_UseVietnamLocation(t *testing.T) {
	now := Now()
	if now.Location().String() != Location.String() {
		t.Fatalf("Now() location=%s want %s", now.Location(), Location)
	}
	if TodayString() != Today().Format("2006-01-02") {
		t.Fatalf("TodayString=%q != Today().Format", TodayString())
	}
}
