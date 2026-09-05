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
	checkInReminderJobName    = "checkin_reminder_refresh"
	checkInReminderCheckEvery = 1 * time.Hour
)

// CheckInReminderJob recomputes D0/D1 reminder flags once per Vietnam civil day.
// GET /me/check-in-reminder also computes live, so this job is the batch path
// for future email/push and for keeping the snapshot table current.
type CheckInReminderJob struct {
	svc   *checkinreminderuc.Service
	locks JobLockStore

	mu         sync.Mutex
	lastRunDay string
	checkEvery time.Duration
}

// NewCheckInReminderJob wires the daily refresh. locks may be nil.
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
	dayKey := streaktime.TodayString()

	j.mu.Lock()
	already := j.lastRunDay == dayKey
	j.mu.Unlock()
	if already {
		return
	}

	if j.locks != nil {
		claimed, err := j.locks.TryClaim(ctx, checkInReminderJobName, dayKey)
		if err != nil {
			slog.Error("checkin_reminder_job: lock claim failed",
				"day", dayKey,
				"error", err.Error(),
			)
			return
		}
		if !claimed {
			slog.Info("checkin_reminder_job: skipped — another replica claimed", "day", dayKey)
			j.mu.Lock()
			j.lastRunDay = dayKey
			j.mu.Unlock()
			return
		}
	}

	started := time.Now().UTC()
	slog.Info("checkin_reminder_job: start", "day", dayKey)

	res, err := j.svc.RefreshWindow(ctx)
	elapsed := time.Since(started)
	if err != nil {
		slog.Error("checkin_reminder_job: fail",
			"day", dayKey,
			"elapsed_ms", elapsed.Milliseconds(),
			"error", err.Error(),
		)
		if j.locks != nil {
			_ = j.locks.ReleaseClaim(ctx, checkInReminderJobName, dayKey)
		}
		return
	}

	j.mu.Lock()
	j.lastRunDay = dayKey
	j.mu.Unlock()

	slog.Info("checkin_reminder_job: end",
		"day", dayKey,
		"scanned", res.Scanned,
		"due_d0", res.DueD0,
		"due_d1", res.DueD1,
		"cleared", res.Cleared,
		"elapsed_ms", elapsed.Milliseconds(),
	)
}
