package scheduler

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
)

func TestCheckInReminderHourKeyFitsLockColumn(t *testing.T) {
	// Production Postgres rejected "2026-09-07-04" on VARCHAR(10) (SQLSTATE 22001),
	// so the hourly job never claimed and never delivered after boot.
	hourKey := streaktime.Now().Format(domain.PushJobHourKeyLayout)
	if len(hourKey) > domain.PushJobRunKeyMaxLen {
		t.Fatalf("hour key %q len=%d exceeds last_run_date VARCHAR(%d)",
			hourKey, len(hourKey), domain.PushJobRunKeyMaxLen)
	}
	sample := time.Date(2026, 9, 7, 4, 0, 0, 0, time.UTC).Format(domain.PushJobHourKeyLayout)
	if sample != "2026-09-07-04" {
		t.Fatalf("layout=%q", sample)
	}
	if len(sample) != 13 {
		t.Fatalf("sample len=%d want 13", len(sample))
	}
}
