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
	checkInReminderJobName    = "checkin_reminder_hour"
	checkInReminderCheckEvery = 1 * time.Hour
	// checkInReminderHourLayout is Vietnam YYYYMMDDHH (10 chars) so the claim
	// fits push_job_locks.last_run_date VARCHAR(10). The dashed layout
	// "2006-01-02-15" is 13 chars and Postgres rejects it (SQLSTATE 22001),
	// which left the hourly email/push fan-out dead after boot.
	checkInReminderHourLayout = "2006010215"
)

// CheckInReminderJob recomputes D0/D1 flags every Vietnam hour, then fans out
// outbound email + typed d0_reminder / d1_reminder push. GET /me/check-in-reminder
// still computes live for the in-app banner.
type CheckInReminderJob struct {
	svc   *checkinreminderuc.Service
	locks JobLockStore

	mu          sync.Mutex
	lastRunHour string
	checkEvery  time.Duration
}

// NewCheckInReminderJob wires the hourly refresh + outbound deliver. locks may be nil.
func NewCheckInReminderJob(svc *checkinreminderuc.Service, locks JobLockStore) *CheckInReminderJob {
	return &CheckInReminderJob{
		svc:        svc,
		locks:      locks,
		checkEvery: checkInReminderCheckEvery,
	}
}

// Start launches the ticker loop in a background goroutine.
func (j *CheckInReminderJob) Start(ctx context.Context) {
	if j == nil || j.svc == nil {
		slog.Warn("checkin_reminder_job: not started — service missing")
		return
	}
	slog.Info("checkin_reminder_job: started",
		"check_every", j.checkEvery.String(),
		"timezone", streaktime.Location.String(),
		"persistent_lock", j.locks != nil,
	)
	go j.loop(ctx)
}

func (j *CheckInReminderJob) loop(ctx context.Context) {
	j.maybeRun(ctx)

	ticker := time.NewTicker(j.checkEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("checkin_reminder_job: stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			j.maybeRun(ctx)
		}
	}
}

func (j *CheckInReminderJob) maybeRun(ctx context.Context) {
	hourKey := streaktime.Now().Format(checkInReminderHourLayout)

	j.mu.Lock()
	already := j.lastRunHour == hourKey
	j.mu.Unlock()
	if already {
		return
	}

	if j.locks != nil {
		claimed, err := j.locks.TryClaim(ctx, checkInReminderJobName, hourKey)
		if err != nil {
			slog.Error("checkin_reminder_job: lock claim failed",
				"hour", hourKey,
				"error", err.Error(),
			)
			return
		}
		if !claimed {
			slog.Info("checkin_reminder_job: skipped — another replica claimed", "hour", hourKey)
			j.mu.Lock()
			j.lastRunHour = hourKey
			j.mu.Unlock()
			return
		}
	}

	started := time.Now().UTC()
	slog.Info("checkin_reminder_job: start", "hour", hourKey, "day", streaktime.TodayString())

	res, err := j.svc.RefreshAndDeliver(ctx)
	elapsed := time.Since(started)
	if err != nil {
		slog.Error("checkin_reminder_job: fail",
			"hour", hourKey,
			"elapsed_ms", elapsed.Milliseconds(),
			"error", err.Error(),
			"email_sent", res.Delivery.EmailSent,
			"email_failed", res.Delivery.EmailFailed,
			"push_sent", res.Delivery.PushSent,
			"push_failed", res.Delivery.PushFailed,
		)
		if j.locks != nil {
			_ = j.locks.ReleaseClaim(ctx, checkInReminderJobName, hourKey)
		}
		return
	}

	j.mu.Lock()
	j.lastRunHour = hourKey
	j.mu.Unlock()

	if res.Delivery.EmailFailed > 0 || res.Delivery.PushFailed > 0 {
		if j.locks != nil {
			_ = j.locks.ReleaseClaim(ctx, checkInReminderJobName, hourKey)
		}
		j.mu.Lock()
		if j.lastRunHour == hourKey {
			j.lastRunHour = ""
		}
		j.mu.Unlock()
		slog.Info("checkin_reminder_job: unclaimed for retry", "hour", hourKey)
	}

	slog.Info("checkin_reminder_job: end",
		"hour", hourKey,
		"day", streaktime.TodayString(),
		"scanned", res.Scanned,
		"due_d0", res.DueD0,
		"due_d1", res.DueD1,
		"cleared", res.Cleared,
		"email_sent", res.Delivery.EmailSent,
		"email_skipped", res.Delivery.EmailSkipped,
		"email_failed", res.Delivery.EmailFailed,
		"push_sent", res.Delivery.PushSent,
		"push_skipped", res.Delivery.PushSkipped,
		"push_failed", res.Delivery.PushFailed,
		"elapsed_ms", elapsed.Milliseconds(),
	)
}
