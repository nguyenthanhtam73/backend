package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
)

const (
	planExpiryJobName    = "plan_expiry_downgrade"
	planExpiryCheckEvery = 1 * time.Hour
)

// PlanExpiryJob runs once per UTC day and downgrades users whose plan_expires_at
// has passed. Feature gates already treat expired rows as Free via EffectivePlanTier;
// this job keeps plan_tier in the DB consistent.
type PlanExpiryJob struct {
	premium *premiumuc.Service
	locks   JobLockStore

	mu         sync.Mutex
	lastRunDay string // "2006-01-02" UTC
	checkEvery time.Duration
}

// NewPlanExpiryJob wires the daily expiry cron. locks may be nil (single-process).
func NewPlanExpiryJob(premium *premiumuc.Service, locks JobLockStore) *PlanExpiryJob {
	return &PlanExpiryJob{
		premium:    premium,
		locks:      locks,
		checkEvery: planExpiryCheckEvery,
	}
}

// Start launches the ticker loop in a background goroutine.
func (j *PlanExpiryJob) Start(ctx context.Context) {
	if j == nil || j.premium == nil {
		slog.Warn("plan_expiry_job: not started — premium service missing")
		return
	}
	slog.Info("plan_expiry_job: started",
		"check_every", j.checkEvery.String(),
		"timezone", "UTC",
		"persistent_lock", j.locks != nil,
	)
	go j.loop(ctx)
}

func (j *PlanExpiryJob) loop(ctx context.Context) {
	j.maybeRun(ctx)

	ticker := time.NewTicker(j.checkEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("plan_expiry_job: stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			j.maybeRun(ctx)
		}
	}
}

func (j *PlanExpiryJob) maybeRun(ctx context.Context) {
	now := time.Now().UTC()
	dayKey := now.Format("2006-01-02")

	j.mu.Lock()
	already := j.lastRunDay == dayKey
	j.mu.Unlock()
	if already {
		return
	}

	if j.locks != nil {
		claimed, err := j.locks.TryClaim(ctx, planExpiryJobName, dayKey)
		if err != nil {
			slog.Error("plan_expiry_job: lock claim failed", "error", err)
			return
		}
		if !claimed {
			slog.Info("plan_expiry_job: skipped — another replica claimed", "day", dayKey)
			j.mu.Lock()
			j.lastRunDay = dayKey
			j.mu.Unlock()
			return
		}
	}

	slog.Info("plan_expiry_job: running", "day", dayKey)
	n, err := j.premium.DowngradeExpiredPlans(ctx)
	if err != nil {
		slog.Error("plan_expiry_job: downgrade failed", "day", dayKey, "error", err)
		if j.locks != nil {
			_ = j.locks.ReleaseClaim(ctx, planExpiryJobName, dayKey)
		}
		return
	}

	j.mu.Lock()
	j.lastRunDay = dayKey
	j.mu.Unlock()

	slog.Info("plan_expiry_job: complete", "day", dayKey, "downgraded", n)
}
