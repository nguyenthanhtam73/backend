package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
	checkinreminderuc "github.com/dadiary/backend/internal/usecase/checkinreminder"
)

func TestEveningEmailWindow(t *testing.T) {
	loc := streaktime.Location
	cases := []struct {
		name string
		now  time.Time
		want eveningWindow
	}{
		{"00:30 ICT waits", time.Date(2026, 10, 5, 0, 30, 0, 0, loc), eveningWindowWaiting},
		{"19:29 ICT waits", time.Date(2026, 10, 5, 19, 29, 0, 0, loc), eveningWindowWaiting},
		{"19:30 ICT is eligible", time.Date(2026, 10, 5, 19, 30, 0, 0, loc), eveningWindowOpen},
		{"20:00 ICT is still eligible", time.Date(2026, 10, 5, 20, 0, 0, 0, loc), eveningWindowOpen},
		{"21:29 ICT is still eligible", time.Date(2026, 10, 5, 21, 29, 0, 0, loc), eveningWindowOpen},
		{"21:30 ICT is closed", time.Date(2026, 10, 5, 21, 30, 0, 0, loc), eveningWindowMissed},
		{"23:30 ICT is closed", time.Date(2026, 10, 5, 23, 30, 0, 0, loc), eveningWindowMissed},
		// 12:29 UTC == 19:29 ICT. 12:30 UTC == 19:30 ICT. 14:30 UTC == 21:30 ICT.
		{"utc 12:29 waits", time.Date(2026, 10, 5, 12, 29, 0, 0, time.UTC), eveningWindowWaiting},
		{"utc 12:30 is open", time.Date(2026, 10, 5, 12, 30, 0, 0, time.UTC), eveningWindowOpen},
		{"utc 14:30 is closed", time.Date(2026, 10, 5, 14, 30, 0, 0, time.UTC), eveningWindowMissed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eveningEmailWindow(tc.now)
			if got != tc.want {
				t.Fatalf("got %v want %v at %s", got, tc.want, tc.now.Format(time.RFC3339))
			}
		})
	}
	if len(streaktime.TodayString()) != 10 {
		t.Fatal("evening job lock key must fit push_job_locks.last_run_date VARCHAR(10)")
	}
}

type recordingEveningSender struct {
	calls int
}

func (r *recordingEveningSender) DeliverEveningEmails(context.Context) (checkinreminderuc.DeliveryResult, error) {
	r.calls++
	return checkinreminderuc.DeliveryResult{EmailSent: 1}, nil
}

type recordingEveningLocks struct {
	claims   int
	releases int
}

func (r *recordingEveningLocks) TryClaim(context.Context, string, string) (bool, error) {
	r.claims++
	return true, nil
}

func (r *recordingEveningLocks) ReleaseClaim(context.Context, string, string) error {
	r.releases++
	return nil
}

func TestEveningCheckInEmail_SendsOnlyInsideWindow(t *testing.T) {
	loc := streaktime.Location
	open := []time.Time{
		time.Date(2026, 10, 5, 19, 30, 0, 0, loc),
		time.Date(2026, 10, 5, 20, 0, 0, 0, loc),
	}
	for _, now := range open {
		sender := &recordingEveningSender{}
		j := &EveningCheckInEmailJob{svc: sender, now: func() time.Time { return now }}
		j.maybeRun(context.Background())
		if sender.calls != 1 {
			t.Fatalf("%s calls=%d want 1", now.Format("15:04"), sender.calls)
		}
	}

	closed := []time.Time{
		time.Date(2026, 10, 5, 21, 30, 0, 0, loc),
		time.Date(2026, 10, 5, 23, 30, 0, 0, loc),
	}
	for _, now := range closed {
		sender := &recordingEveningSender{}
		locks := &recordingEveningLocks{}
		j := &EveningCheckInEmailJob{
			svc:   sender,
			locks: locks,
			now:   func() time.Time { return now },
		}
		j.maybeRun(context.Background())
		if sender.calls != 0 {
			t.Fatalf("%s sent %d", now.Format("15:04"), sender.calls)
		}
		if locks.claims != 1 || locks.releases != 0 {
			t.Fatalf("%s claims=%d releases=%d — missed day must stay closed", now.Format("15:04"), locks.claims, locks.releases)
		}
	}
}

func TestEveningCheckInEmail_BootAfter2130DoesNotSendOrDeferToMidnight(t *testing.T) {
	loc := streaktime.Location
	sender := &recordingEveningSender{}
	locks := &recordingEveningLocks{}
	when := time.Date(2026, 10, 5, 23, 30, 0, 0, loc)
	j := &EveningCheckInEmailJob{
		svc:   sender,
		locks: locks,
		now:   func() time.Time { return when },
	}

	// Boot / immediate maybeRun, same path Start uses on process start.
	j.maybeRun(context.Background())
	j.maybeRun(context.Background())
	if sender.calls != 0 {
		t.Fatalf("boot at 23:30 sent %d", sender.calls)
	}
	if locks.claims != 1 || locks.releases != 0 {
		t.Fatalf("boot claims=%d releases=%d", locks.claims, locks.releases)
	}

	// Next civil day's early morning must not flush the missed evening.
	when = time.Date(2026, 10, 6, 0, 5, 0, 0, loc)
	j.maybeRun(context.Background())
	if sender.calls != 0 {
		t.Fatalf("00:05 ICT sent a deferred evening email (%d)", sender.calls)
	}
	if locks.claims != 1 {
		t.Fatalf("midnight claimed a new day (%d)", locks.claims)
	}
}
