// Package streaktime defines the civil calendar used for streak / check-in days
// and for evening push jobs (daily reminder, streak at risk).
//
// DaDiary users are primarily in Vietnam; using Asia/Ho_Chi_Minh avoids the
// UTC midnight skew (evening VN still counting as "yesterday" in UTC).
// All "today" / "yesterday" product logic should go through this package so the
// job clock and SkinCheck/streak rows share one Vietnam civil day.
package streaktime

import "time"

// Location is the calendar timezone for streak, SkinCheck check_date, and
// DailyReminderJob scheduling (Asia/Ho_Chi_Minh).
var Location = mustLoad("Asia/Ho_Chi_Minh")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// Tests / minimal environments without zoneinfo: fixed +07.
		return time.FixedZone(name, 7*60*60)
	}
	return loc
}

// Now returns the current instant in Location (Vietnam wall clock).
func Now() time.Time {
	return time.Now().In(Location)
}

// Today returns today's civil date in Location, stored as UTC midnight of that
// Y-M-D (date-only semantics for Postgres `date` columns).
func Today() time.Time {
	return DateOf(time.Now())
}

// TodayString returns today's Vietnam civil date as "2006-01-02".
// Use for job last-run keys and once-per-day dedupe (same day as Today()).
func TodayString() string {
	return Today().Format("2006-01-02")
}

// DateOf maps an instant to the civil calendar day in Location.
func DateOf(t time.Time) time.Time {
	local := t.In(Location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// RoutineDayRollHour is when the routine care day rolls, Vietnam wall clock.
// Morning opens at 04:00. Until then the evening session still belongs to
// the previous civil day. Check-ins stay on DateOf (civil midnight).
const RoutineDayRollHour = 4

// RoutineCareDay is the calendar day a routine tick is stored on.
// From 00:00 until 04:00 Asia/Ho_Chi_Minh it is the previous civil day.
// At 04:00 it becomes that morning's civil day. The result is UTC midnight
// of that Y-M-D, matching routine_entries.routine_date.
// The daily quest chest uses the same day: an evening claim after midnight
// and before 04:00 still belongs to the previous care day, and both sessions
// of that day stay claimable until the roll.
func RoutineCareDay(now time.Time) time.Time {
	local := now.In(Location)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	if local.Hour() < RoutineDayRollHour {
		return day.AddDate(0, 0, -1)
	}
	return day
}
