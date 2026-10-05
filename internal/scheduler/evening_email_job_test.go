package scheduler

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
)

func TestEveningCheckInEmailWindow(t *testing.T) {
	loc := streaktime.Location
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"00:30 ICT is before the evening send", time.Date(2026, 10, 5, 0, 30, 0, 0, loc), false},
		{"19:29 ICT waits", time.Date(2026, 10, 5, 19, 29, 0, 0, loc), false},
		{"19:30 ICT sends", time.Date(2026, 10, 5, 19, 30, 0, 0, loc), true},
		{"later the same evening still sends once", time.Date(2026, 10, 5, 21, 5, 0, 0, loc), true},
		// 12:29 UTC == 19:29 ICT.
		{"utc 12:29 is still before 19:30 ICT", time.Date(2026, 10, 5, 12, 29, 0, 0, time.UTC).In(loc), false},
		// 12:30 UTC == 19:30 ICT.
		{"utc 12:30 is 19:30 ICT", time.Date(2026, 10, 5, 12, 30, 0, 0, time.UTC).In(loc), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRunDailyReminder(tc.now, eveningCheckInEmailHour, eveningCheckInEmailMinute)
			if got != tc.want {
				t.Fatalf("got %v want %v at %s", got, tc.want, tc.now.Format(time.RFC3339))
			}
		})
	}
	if len(streaktime.TodayString()) != 10 {
		t.Fatal("evening job lock key must fit push_job_locks.last_run_date VARCHAR(10)")
	}
}
