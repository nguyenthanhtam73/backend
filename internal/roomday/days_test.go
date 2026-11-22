package roomday

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

func TestCountMemberDaysVNBoundaryAndOncePerDay(t *testing.T) {
	loc := streaktime.Location
	// Room starts 1 Nov 10:00 VN and ends 8 days later, 9 Nov 10:00 VN.
	start := time.Date(2026, 11, 1, 10, 0, 0, 0, loc)
	end := start.Add(8 * 24 * time.Hour)
	if got := SpanDays(start, end); got != 9 {
		t.Fatalf("span days %d, want 9 (partial first and last civil dates)", got)
	}
	midnight := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	if got := SpanDays(midnight, midnight.Add(8*24*time.Hour)); got != 8 {
		t.Fatalf("midnight span %d, want 8", got)
	}

	user := uuid.New()
	other := uuid.New()
	// 2 Nov 16:30 UTC is still 2 Nov in Vietnam. 2 Nov 17:30 UTC is 3 Nov 00:30 VN.
	lateUTC := time.Date(2026, 11, 2, 16, 30, 0, 0, time.UTC)
	earlyNext := time.Date(2026, 11, 2, 17, 30, 0, 0, time.UTC)
	if streaktime.DateOf(lateUTC).Format("2006-01-02") != "2026-11-02" {
		t.Fatalf("late utc date %s", streaktime.DateOf(lateUTC))
	}
	if streaktime.DateOf(earlyNext).Format("2006-01-02") != "2026-11-03" {
		t.Fatalf("next vn date %s", streaktime.DateOf(earlyNext))
	}

	members := []MemberSpan{{UserID: user, JoinedAt: start}, {UserID: other, JoinedAt: start}}
	cared := map[uuid.UUID][]time.Time{
		user: {streaktime.DateOf(lateUTC), streaktime.DateOf(lateUTC)},
	}
	checked := map[uuid.UUID][]time.Time{
		user:  {streaktime.DateOf(lateUTC), streaktime.DateOf(earlyNext)},
		other: {streaktime.DateOf(earlyNext)},
	}
	if got := CountMemberDays(start, end, members, cared, checked); got != 3 {
		t.Fatalf("member-days %d, want 3 (user 2 Nov + 3 Nov, other 3 Nov)", got)
	}

	earlyJoin := []MemberSpan{{UserID: user, JoinedAt: time.Date(2026, 11, 4, 8, 0, 0, 0, loc)}}
	if got := CountMemberDays(start, end, earlyJoin, nil, checked); got != 0 {
		t.Fatalf("before join %d", got)
	}

	outside := map[uuid.UUID][]time.Time{user: {time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)}}
	if got := CountMemberDays(start, end, members[:1], nil, outside); got != 0 {
		t.Fatalf("outside room %d", got)
	}

	// On Vietnam 3 Nov the 2 Nov routine is no longer today, even though
	// 17:30 UTC is still 2 Nov on a UTC calendar.
	caredToday, checkedToday := TodayFlags(earlyNext, cared[user], checked[user])
	if caredToday || !checkedToday {
		t.Fatalf("3 Nov flags cared %v checked %v", caredToday, checkedToday)
	}
	caredToday, checkedToday = TodayFlags(lateUTC, cared[user], checked[user])
	if !caredToday || !checkedToday {
		t.Fatalf("2 Nov flags cared %v checked %v", caredToday, checkedToday)
	}
}
