package reminder

import (
	"strings"
	"time"
	// Zone data for time.LoadLocation. The server image has no zoneinfo.
	_ "time/tzdata"

	"github.com/dadiary/backend/internal/streaktime"
)

// CaptureGrace is how long after the saved HH:MM a tick may still send.
// The window never crosses local midnight, so a 23:30 reminder stops at
// 00:00 even though 23:30+2h would be 01:30.
const CaptureGrace = 2 * time.Hour

// CaptureTick is how often the scheduler wakes. Five minutes keeps the send
// inside about ten minutes after the saved HH:MM.
const CaptureTick = 5 * time.Minute

// SavedSchedule is reminder_enabled true and a valid local HH:MM.
// NULL enabled, false, and true without a clock are not a saved schedule.
func SavedSchedule(enabled *bool, hhmm *string) bool {
	if enabled == nil || !*enabled || hhmm == nil {
		return false
	}
	_, _, ok := ParseClock(*hhmm)
	return ok
}

// ParseClock accepts HH:MM from 00:00 through 23:59. The hour and minute
// must already be zero-padded, matching the value PUT /me/reminder stores.
func ParseClock(raw string) (hour, minute int, ok bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) != 5 || raw[2] != ':' {
		return 0, 0, false
	}
	hour, okH := twoDigits(raw[:2])
	minute, okM := twoDigits(raw[3:])
	if !okH || !okM || hour > 23 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func twoDigits(raw string) (int, bool) {
	if len(raw) != 2 || raw[0] < '0' || raw[0] > '9' || raw[1] < '0' || raw[1] > '9' {
		return 0, false
	}
	return int(raw[0]-'0')*10 + int(raw[1]-'0'), true
}

// Zone returns the IANA location for a stored timezone.
// A nil, blank, "Local", or unknown name uses Asia/Ho_Chi_Minh (streaktime).
func Zone(name *string) *time.Location {
	if name == nil {
		return streaktime.Location
	}
	n := strings.TrimSpace(*name)
	if n == "" || strings.EqualFold(n, "Local") {
		return streaktime.Location
	}
	loc, err := time.LoadLocation(n)
	if err != nil {
		return streaktime.Location
	}
	return loc
}

// OpenWindow reports whether now is inside the user's send window.
//
// The window is [HH:MM, HH:MM+grace] on the user's local civil date, clipped
// so it ends before the next local midnight. localDate is that civil date
// even when the window is closed. A HH:MM that does not exist that day
// (a DST spring-forward gap) is closed: the send does not move to another hour.
func OpenWindow(now time.Time, loc *time.Location, hhmm string, grace time.Duration) (localDate string, open bool) {
	if loc == nil {
		loc = streaktime.Location
	}
	if grace <= 0 {
		grace = CaptureGrace
	}
	local := now.In(loc)
	localDate = local.Format("2006-01-02")
	hour, minute, ok := ParseClock(hhmm)
	if !ok {
		return localDate, false
	}
	start, exists := wallClock(loc, local.Year(), local.Month(), local.Day(), hour, minute)
	if !exists {
		return localDate, false
	}
	end := start.Add(grace)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	if end.After(midnight) {
		end = midnight
	}
	if local.Before(start) || local.After(end) || !local.Before(midnight) {
		return localDate, false
	}
	return localDate, true
}

// LocalDay returns [start, end) for the civil date of now in loc.
func LocalDay(now time.Time, loc *time.Location) (start, end time.Time) {
	if loc == nil {
		loc = streaktime.Location
	}
	local := now.In(loc)
	start = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return start, start.AddDate(0, 0, 1)
}

// wallClock builds HH:MM on a civil date. ok is false when that wall time
// does not exist (DST spring forward). On a fall-back fold, Go's time.Date
// returns the earlier instance; the grace window covers the later one, and
// the per-day claim keeps the send to one.
func wallClock(loc *time.Location, year int, month time.Month, day, hour, minute int) (time.Time, bool) {
	t := time.Date(year, month, day, hour, minute, 0, 0, loc)
	got := t.In(loc)
	if got.Year() != year || got.Month() != month || got.Day() != day || got.Hour() != hour || got.Minute() != minute {
		return time.Time{}, false
	}
	return t, true
}
