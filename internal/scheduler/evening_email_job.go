package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
	checkinreminderuc "github.com/dadiary/backend/internal/usecase/checkinreminder"
)

const (
	eveningCheckInEmailJobName = "checkin_email_evening"
	// Send only while Vietnam local time is >= 19:30 and < 21:30.
	// 21:30 itself is outside the window. A wake after that closes the civil
	// day without sending, so a 23:30 deploy cannot flush mail near midnight.
	eveningCheckInEmailStartHour   = 19
	eveningCheckInEmailStartMinute = 30
	eveningCheckInEmailEndHour     = 21
	eveningCheckInEmailEndMinute   = 30
	eveningCheckInEmailEvery       = 30 * time.Minute
)

// eveningEmailDeliverer is the 19:30 D1/Day-3 send. *checkinreminder.Service implements it.
type eveningEmailDeliverer interface {
	DeliverEveningEmails(ctx context.Context) (checkinreminderuc.DeliveryResult, error)
}

// EveningCheckInEmailJob sends D1 and Day-3 reminder emails once per Vietnam
// civil day, only inside 19:30–21:30 Asia/Ho_Chi_Minh. It does not send on the
// hourly midnight tick and does not catch up after 21:30. D0 email stays on
// CheckInReminderJob.
type EveningCheckInEmailJob struct {
	svc   eveningEmailDeliverer
	locks JobLockStore
	now   func() time.Time

	mu          sync.Mutex
	lastRunDate string
	checkEvery  time.Duration
}

// NewEveningCheckInEmailJob wires the 19:30 ICT email pass. locks may be nil.
func NewEveningCheckInEmailJob(svc *checkinreminderuc.Service, locks JobLockStore) *EveningCheckInEmailJob {
	j := &EveningCheckInEmailJob{
		locks:      locks,
		now:        streaktime.Now,
		checkEvery: eveningCheckInEmailEvery,
	}
	if svc != nil {
		j.svc = svc
	}
	return j
}

// Start launches the ticker. A boot inside 19:30–21:30 ICT still sends once.
// A boot at or after 21:30 closes that Vietnam day without sending.
func (j *EveningCheckInEmailJob) Start(ctx context.Context) {
	if j == nil || j.svc == nil {
		slog.Warn("evening_checkin_email_job: not started — service missing")
		return
	}
	slog.Info("evening_checkin_email_job: started",
		"window", "19:30-21:30",
		"timezone", streaktime.Location.String(),
		"check_every", j.checkEvery.String(),
		"persistent_lock", j.locks != nil,
	)
	go j.loop(ctx)
}

func (j *EveningCheckInEmailJob) loop(ctx context.Context) {
	j.maybeRun(ctx)
	ticker := time.NewTicker(j.checkEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("evening_checkin_email_job: stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			j.maybeRun(ctx)
		}
	}
}

func (j *EveningCheckInEmailJob) clock() time.Time {
	if j != nil && j.now != nil {
		return j.now()
	}
	return streaktime.Now()
}

func (j *EveningCheckInEmailJob) maybeRun(ctx context.Context) {
	now := j.clock().In(streaktime.Location)
	today := streaktime.DateOf(now).Format("2006-01-02")
	switch eveningEmailWindow(now) {
	case eveningWindowWaiting:
		slog.Debug("evening_checkin_email_job: waiting for 19:30 ICT",
			"now", now.Format(time.RFC3339),
		)
		return
	case eveningWindowMissed:
		// Close this civil day so a later tick (including one near midnight)
		// cannot treat the missed 19:30 slot as still pending.
		j.closeDayWithoutSend(ctx, today, now)
		return
	}
	if !j.claim(ctx, today) {
		return
	}

	slog.Info("evening_checkin_email_job: start", "date", today)
	res, err := j.svc.DeliverEveningEmails(ctx)
	if err != nil {
		slog.Error("evening_checkin_email_job: fail",
			"date", today,
			"email_sent", res.EmailSent,
			"email_failed", res.EmailFailed,
			"err", err,
		)
	} else {
		slog.Info("evening_checkin_email_job: end",
			"date", today,
			"candidates", res.Candidates,
			"email_sent", res.EmailSent,
			"email_skipped", res.EmailSkipped,
			"email_failed", res.EmailFailed,
		)
	}
	if shouldUnclaimPushBatch(res.EmailSent, res.EmailFailed, err) {
		j.release(ctx, today)
		slog.Info("evening_checkin_email_job: unclaimed for retry", "date", today)
	}
}

// closeDayWithoutSend records the Vietnam day as finished and does not deliver.
// The claim is not released: releasing it would leave the day pending for a
// later tick in the same night.
func (j *EveningCheckInEmailJob) closeDayWithoutSend(ctx context.Context, today string, now time.Time) {
	j.mu.Lock()
	if j.lastRunDate == today {
		j.mu.Unlock()
		slog.Debug("evening_checkin_email_job: already closed for today", "date", today)
		return
	}
	j.lastRunDate = today
	j.mu.Unlock()

	if j.locks != nil {
		ok, err := j.locks.TryClaim(ctx, eveningCheckInEmailJobName, today)
		if err != nil {
			slog.Error("evening_checkin_email_job: lock claim failed while closing window",
				"date", today,
				"err", err,
			)
			j.clearMem(today)
			return
		}
		if !ok {
			slog.Info("evening_checkin_email_job: day already claimed, not sending after window",
				"date", today,
			)
			return
		}
	}
	slog.Info("evening_checkin_email_job: skipped — outside 19:30–21:30 ICT",
		"date", today,
		"now", now.Format(time.RFC3339),
	)
}

func (j *EveningCheckInEmailJob) claim(ctx context.Context, today string) bool {
	j.mu.Lock()
	if j.lastRunDate == today {
		j.mu.Unlock()
		return false
	}
	j.lastRunDate = today
	j.mu.Unlock()

	if j.locks == nil {
		return true
	}
	ok, err := j.locks.TryClaim(ctx, eveningCheckInEmailJobName, today)
	if err != nil {
		slog.Error("evening_checkin_email_job: lock claim failed", "date", today, "err", err)
		j.clearMem(today)
		return false
	}
	if !ok {
		slog.Info("evening_checkin_email_job: skipped — another replica claimed", "date", today)
		j.clearMem(today)
		return false
	}
	return true
}

func (j *EveningCheckInEmailJob) clearMem(today string) {
	j.mu.Lock()
	if j.lastRunDate == today {
		j.lastRunDate = ""
	}
	j.mu.Unlock()
}

func (j *EveningCheckInEmailJob) release(ctx context.Context, today string) {
	j.clearMem(today)
	if j.locks == nil {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pushJobReleaseTimeout)
	defer cancel()
	if err := j.locks.ReleaseClaim(releaseCtx, eveningCheckInEmailJobName, today); err != nil {
		slog.Error("evening_checkin_email_job: release failed", "date", today, "err", err)
	}
}

type eveningWindow int

const (
	eveningWindowWaiting eveningWindow = iota
	eveningWindowOpen
	eveningWindowMissed
)

// eveningEmailWindow classifies Vietnam local time.
// Open is 19:30 inclusive through 21:30 exclusive. At or after 21:30 the day is missed.
func eveningEmailWindow(now time.Time) eveningWindow {
	now = now.In(streaktime.Location)
	mins := now.Hour()*60 + now.Minute()
	open := eveningCheckInEmailStartHour*60 + eveningCheckInEmailStartMinute
	end := eveningCheckInEmailEndHour*60 + eveningCheckInEmailEndMinute
	switch {
	case mins < open:
		return eveningWindowWaiting
	case mins >= end:
		return eveningWindowMissed
	default:
		return eveningWindowOpen
	}
}
