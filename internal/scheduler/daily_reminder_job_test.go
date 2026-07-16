package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
)

type stubJobLocks struct {
	ok  bool
	err error

	// releaseSawLiveCtx is set when ReleaseClaim receives a non-canceled ctx.
	releaseSawLiveCtx bool
	releaseCalled     bool
}

func (s *stubJobLocks) TryClaim(context.Context, string, string) (bool, error) {
	return s.ok, s.err
}

func (s *stubJobLocks) ReleaseClaim(ctx context.Context, _, _ string) error {
	s.releaseCalled = true
	if ctx.Err() == nil {
		s.releaseSawLiveCtx = true
	}
	return ctx.Err()
}

func TestRelease_UsesLiveCtxWhenParentCanceled(t *testing.T) {
	locks := &stubJobLocks{}
	j := &DailyReminderJob{locks: locks}
	today := "2026-07-17"
	mem := today

	parent, cancel := context.WithCancel(context.Background())
	cancel() // simulate SIGTERM mid-fan-out

	j.release(parent, "daily_reminder", today, &mem)

	if mem != "" {
		t.Fatalf("memDate=%q want cleared", mem)
	}
	if !locks.releaseCalled {
		t.Fatal("ReleaseClaim was not called")
	}
	if !locks.releaseSawLiveCtx {
		t.Fatal("ReleaseClaim still saw canceled ctx — claim would stick all evening")
	}
}

func TestClaim_ClearsMemDateWhenPeerAlreadyClaimed(t *testing.T) {
	j := &DailyReminderJob{locks: &stubJobLocks{ok: false}}
	today := "2026-07-17"
	var mem string

	if j.claim(context.Background(), "daily_reminder", today, &mem) {
		t.Fatal("claim should fail when peer already claimed")
	}
	if mem != "" {
		t.Fatalf("memDate stuck at %q — peer released claim could never be retried", mem)
	}

	// Winning claim keeps memDate for the day.
	j.locks = &stubJobLocks{ok: true}
	if !j.claim(context.Background(), "daily_reminder", today, &mem) {
		t.Fatal("claim should succeed")
	}
	if mem != today {
		t.Fatalf("memDate=%q want %q after winning claim", mem, today)
	}
}

func TestNewDailyReminderJob_UsesVietnamLocation(t *testing.T) {
	j := NewDailyReminderJob(nil, nil, nil)
	if j.loc != streaktime.Location {
		t.Fatalf("loc=%v want streaktime.Location (%s)", j.loc, streaktime.Location)
	}
}

func TestShouldUnclaimPushBatch(t *testing.T) {
	cancelErr := context.Canceled
	cases := []struct {
		name   string
		sent   int
		failed int
		err    error
		want   bool
	}{
		{name: "setup/list fail", sent: 0, failed: 0, err: context.DeadlineExceeded, want: true},
		{name: "cancel before first send", sent: 0, failed: 0, err: cancelErr, want: true},
		{name: "cancel after skips only", sent: 0, failed: 0, err: cancelErr, want: true},
		{name: "provider outage all failed", sent: 0, failed: 8, err: nil, want: true},
		{name: "cancel after some fails", sent: 0, failed: 2, err: cancelErr, want: true},
		// Receipts protect already-sent users — unclaim to retry the rest.
		{name: "partial sent with fails retries", sent: 3, failed: 2, err: nil, want: true},
		{name: "partial sent with cancel retries", sent: 3, failed: 0, err: cancelErr, want: true},
		{name: "all skipped success", sent: 0, failed: 0, err: nil, want: false},
		{name: "happy path sent", sent: 5, failed: 0, err: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldUnclaimPushBatch(tc.sent, tc.failed, tc.err)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestShouldRunDailyReminder(t *testing.T) {
	// Times are wall-clock in Asia/Ho_Chi_Minh (same as streaktime.Now()).
	loc := streaktime.Location
	cases := []struct {
		name   string
		now    time.Time
		hour   int
		minute int
		want   bool
	}{
		{
			name:   "before window",
			now:    time.Date(2026, 7, 17, 19, 59, 0, 0, loc),
			hour:   20,
			minute: 0,
			want:   false,
		},
		{
			name:   "exact minute",
			now:    time.Date(2026, 7, 17, 20, 0, 0, 0, loc),
			hour:   20,
			minute: 0,
			want:   true,
		},
		{
			name:   "after window same day",
			now:    time.Date(2026, 7, 17, 21, 15, 0, 0, loc),
			hour:   20,
			minute: 0,
			want:   true,
		},
		{
			name:   "custom minute not yet",
			now:    time.Date(2026, 7, 17, 20, 14, 0, 0, loc),
			hour:   20,
			minute: 30,
			want:   false,
		},
		{
			name:   "custom minute reached",
			now:    time.Date(2026, 7, 17, 20, 30, 0, 0, loc),
			hour:   20,
			minute: 30,
			want:   true,
		},
		{
			// 12:00 UTC == 19:00 VN — still before 20:00 VN target.
			name:   "utc noon is before vn evening window",
			now:    time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC).In(loc),
			hour:   20,
			minute: 0,
			want:   false,
		},
		{
			// 13:00 UTC == 20:00 VN — window open.
			name:   "utc 13:00 is vn 20:00 window",
			now:    time.Date(2026, 7, 17, 13, 0, 0, 0, time.UTC).In(loc),
			hour:   20,
			minute: 0,
			want:   true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRunDailyReminder(tc.now, tc.hour, tc.minute)
			if got != tc.want {
				t.Fatalf("got %v want %v for %s", got, tc.want, tc.now)
			}
		})
	}
}
