package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/dadiary/backend/internal/reminder"
	scheduledreminderuc "github.com/dadiary/backend/internal/usecase/scheduledreminder"
)

// ScheduledCaptureJob wakes every few minutes and sends one capture reminder
// to users who saved a local HH:MM. The moment is claimed per user per local
// date, so overlapping ticks and replicas do not double-send. A boot inside
// the grace window still sends; a boot after local midnight does not catch up.
type ScheduledCaptureJob struct {
	svc  *scheduledreminderuc.Service
	mu   sync.Mutex
	busy bool
}

// NewScheduledCaptureJob wires the per-user capture tick.
func NewScheduledCaptureJob(svc *scheduledreminderuc.Service) *ScheduledCaptureJob {
	return &ScheduledCaptureJob{svc: svc}
}

// Start launches the ticker. Cancel ctx to stop.
func (j *ScheduledCaptureJob) Start(ctx context.Context) {
	if j == nil || j.svc == nil {
		slog.Warn("scheduled_capture_job: not started — service missing")
		return
	}
	slog.Info("scheduled_capture_job: started",
		"check_every", reminder.CaptureTick.String(),
		"grace", reminder.CaptureGrace.String(),
		"default_timezone", "Asia/Ho_Chi_Minh",
	)
	go j.loop(ctx)
}

func (j *ScheduledCaptureJob) loop(ctx context.Context) {
	j.maybeRun(ctx)
	ticker := time.NewTicker(reminder.CaptureTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduled_capture_job: stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			j.maybeRun(ctx)
		}
	}
}

func (j *ScheduledCaptureJob) maybeRun(ctx context.Context) {
	j.mu.Lock()
	if j.busy {
		j.mu.Unlock()
		slog.Debug("scheduled_capture_job: skip — previous tick still running")
		return
	}
	j.busy = true
	j.mu.Unlock()
	defer func() {
		j.mu.Lock()
		j.busy = false
		j.mu.Unlock()
	}()

	res, err := j.svc.Deliver(ctx)
	if err != nil {
		slog.Error("scheduled_capture_job: tick failed",
			"candidates", res.Candidates,
			"sent", res.Sent,
			"failed", res.Failed,
			"err", err,
		)
	}
}
