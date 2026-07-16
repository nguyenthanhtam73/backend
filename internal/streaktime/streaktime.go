// Package streaktime defines the civil calendar used for streak / check-in days.
//
// DaDiary users are primarily in Vietnam; using Asia/Ho_Chi_Minh avoids the
// UTC midnight skew (evening VN still counting as "yesterday" in UTC).
package streaktime

import "time"

// Location is the calendar timezone for streak and SkinCheck check_date.
var Location = mustLoad("Asia/Ho_Chi_Minh")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// Tests / minimal environments without zoneinfo: fixed +07.
		return time.FixedZone(name, 7*60*60)
	}
	return loc
}

// Today returns today's civil date in Location, stored as UTC midnight of that
// Y-M-D (date-only semantics for Postgres `date` columns).
func Today() time.Time {
	return DateOf(time.Now())
}

// DateOf maps an instant to the civil calendar day in Location.
func DateOf(t time.Time) time.Time {
	local := t.In(Location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}
