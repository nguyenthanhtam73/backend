package scheduler

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
)

func TestCheckInReminderHourKeyFitsLockColumn(t *testing.T) {
	// Production rejected "2026-09-07-04" (13 chars) on VARCHAR(10).
	// Compact YYYYMMDDHH must stay ≤10 so TryClaim can INSERT.
	const liveColumn = 10
	dashed := time.Date(2026, 9, 7, 4, 0, 0, 0, time.UTC).Format("2006-01-02-15")
	if len(dashed) != 13 {
		t.Fatalf("dashed layout should be the 13-char key that failed in prod: %q", dashed)
	}

	hourKey := streaktime.Now().Format(checkInReminderHourLayout)
	if len(hourKey) > liveColumn {
		t.Fatalf("hour key %q len=%d exceeds last_run_date VARCHAR(%d)",
			hourKey, len(hourKey), liveColumn)
	}
	sample := time.Date(2026, 9, 7, 4, 0, 0, 0, time.UTC).Format(checkInReminderHourLayout)
	if sample != "2026090704" {
		t.Fatalf("layout=%q want 2026090704", sample)
	}
	if len(sample) != liveColumn {
		t.Fatalf("sample %q len=%d want %d", sample, len(sample), liveColumn)
	}
}
