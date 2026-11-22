package roomday

import (
	"time"

	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

// MemberSpan is one current member and the instant they joined.
type MemberSpan struct {
	UserID   uuid.UUID
	JoinedAt time.Time
}

// CountMemberDays sums Vietnam civil days on which a current member either
// finished a routine session or recorded a skin check. Each member contributes
// at most one per civil day. A day counts when it overlaps [start, end) and
// is on or after the member's join date in Asia/Ho_Chi_Minh.
func CountMemberDays(start, end time.Time, members []MemberSpan, cared, checked map[uuid.UUID][]time.Time) int {
	if !end.After(start) {
		return 0
	}
	total := 0
	for _, member := range members {
		if member.UserID == uuid.Nil {
			continue
		}
		joinDay := streaktime.DateOf(member.JoinedAt)
		days := map[string]struct{}{}
		add := func(list []time.Time) {
			for _, raw := range list {
				if raw.IsZero() {
					continue
				}
				day := streaktime.DateOf(raw)
				if day.Before(joinDay) || !civilDayOverlaps(day, start, end) {
					continue
				}
				days[day.Format("2006-01-02")] = struct{}{}
			}
		}
		add(cared[member.UserID])
		add(checked[member.UserID])
		total += len(days)
	}
	return total
}

// SpanDays is how many Vietnam civil dates overlap [start, end).
// An 8×24h room covers 8 dates when it starts at Vietnam midnight, and 9
// when it starts later in the day.
func SpanDays(start, end time.Time) int {
	if !end.After(start) {
		return 0
	}
	n := 0
	day := streaktime.DateOf(start)
	for i := 0; i < 12; i++ {
		if !civilDayOverlaps(day, start, end) {
			if n > 0 {
				return n
			}
			day = day.AddDate(0, 0, 1)
			continue
		}
		n++
		day = day.AddDate(0, 0, 1)
	}
	return n
}

// TodayFlags reports whether lists contain today's Vietnam civil date.
func TodayFlags(now time.Time, cared, checked []time.Time) (caredToday, checkedToday bool) {
	key := streaktime.DateOf(now).Format("2006-01-02")
	for _, raw := range cared {
		if !raw.IsZero() && streaktime.DateOf(raw).Format("2006-01-02") == key {
			caredToday = true
			break
		}
	}
	for _, raw := range checked {
		if !raw.IsZero() && streaktime.DateOf(raw).Format("2006-01-02") == key {
			checkedToday = true
			break
		}
	}
	return caredToday, checkedToday
}

// civilDayOverlaps reports whether the Vietnam calendar date stored in day
// (UTC midnight of that Y-M-D, as streaktime.DateOf returns) meets [start, end).
func civilDayOverlaps(day, start, end time.Time) bool {
	y, m, d := day.UTC().Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, streaktime.Location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	return dayStart.Before(end) && dayEnd.After(start)
}
