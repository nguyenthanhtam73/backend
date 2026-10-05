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
	eveningCheckInEmailHour    = 19
	eveningCheckInEmailMinute  = 30
	eveningCheckInEmailEvery   = 30 * time.Minute
)

// EveningCheckInEmailJob sends D1 and Day-3 reminder emails once per Vietnam
// civil day, at or after 19:30 Asia/Ho_Chi_Minh. It does not send on the
// hourly midnight tick. D0 email stays on CheckInReminderJob.
type EveningCheckInEmailJob struct {
	svc   *checkinreminderuc.Service
	locks JobLockStore

	mu          sync.Mutex
	lastRunDate string
	checkEvery  time.Duration
}

// NewEveningCheckInEmailJob wires the 19:30 ICT email pass. locks may be nil.
func NewEveningCheckInEmailJob(svc *checkinreminderuc.Service, locks JobLockStore) *EveningCheckInEmailJob {
	return &EveningCheckInEmailJob{
		svc:        svc,
		locks:      locks,
		checkEvery: eveningCheckInEmailEvery,
	}
}

// Start launches the ticker. A process that boots after 19:30 still sends once
// if today's claim is free.
func (j *EveningCheckInEmailJob) Start(ctx context.Context) {
	if j == nil || j.svc == nil {
		slog.Warn("evening_checkin_email_job: not started — service missing")
		return
	}
	slog.Info("evening_checkin_email_job: started",
		"hour", eveningCheckInEmailHour,
		"minute", eveningCheckInEmailMinute,
		"check_every", j.checkEvery.String(),
		"timezone", streaktime.Location.String(),
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

func (j *EveningCheckInEmailJob) maybeRun(ctx context.Context) {
	now := streaktime.Now()
	if !shouldRunDailyReminder(now, eveningCheckInEmailHour, eveningCheckInEmailMinute) {
		slog.Debug("evening_checkin_email_job: waiting for 19:30 ICT",
			"now", now.Format(time.RFC3339),
		)
		return
	}
	today := streaktime.TodayString()
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
