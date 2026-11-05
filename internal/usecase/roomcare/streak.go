package roomcare

import (
	"time"

	"github.com/dadiary/backend/internal/streaktime"
)

// maxCareStreak is how many civil days one lookup will count. The routine
// read uses the same window, so a longer run still reports this cap.
const maxCareStreak = 400

// CareStreak counts consecutive Asia/Ho_Chi_Minh days ending at today when
// today is in done, otherwise ending at yesterday. done holds civil days on
// which at least one routine session was fully done. A gap, or a miss today
// and yesterday, stops the run at 0.
func CareStreak(today time.Time, done []time.Time) int {
	today = streaktime.DateOf(today)
	have := make(map[string]struct{}, len(done))
	for _, day := range done {
		if day.IsZero() {
			continue
		}
		have[streaktime.DateOf(day).Format("2006-01-02")] = struct{}{}
	}
	start := today
	if _, ok := have[start.Format("2006-01-02")]; !ok {
		start = start.AddDate(0, 0, -1)
	}
	n := 0
	for {
		if _, ok := have[start.Format("2006-01-02")]; !ok {
			return n
		}
		n++
		if n >= maxCareStreak {
			return n
		}
		start = start.AddDate(0, 0, -1)
	}
}

// Lookback is the oldest civil day a streak query needs for today.
func Lookback(today time.Time) time.Time {
	return streaktime.DateOf(today).AddDate(0, 0, -(maxCareStreak - 1))
}
