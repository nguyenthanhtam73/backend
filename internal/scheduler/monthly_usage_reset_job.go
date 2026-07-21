package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
)

const (
	monthlyUsageResetJobName    = "usage_monthly_reset"
	monthlyUsageResetCheckEvery = 1 * time.Hour
)

// MonthlyUsageResetJob runs shortly after 00:00 UTC on the 1st of each month
// and cleans completed user_usages period rows. Live metering already keys off
// the current UTC month (period_start), so this job is idempotent cleanup + log.
type MonthlyUsageResetJob struct {
	premium *premiumuc.Service
	locks   JobLockStore

	mu           sync.Mutex
	lastRunMonth string // "2006-01" UTC
	checkEvery   time.Duration
}

// NewMonthlyUsageResetJob wires the cron. locks may be nil (single-process only).
func NewMonthlyUsageResetJob(premium *premiumuc.Service, locks JobLockStore) *MonthlyUsageResetJob {
	return &MonthlyUsageResetJob{
		premium:    premium,
		locks:      locks,
		checkEvery: monthlyUsageResetCheckEvery,
	}
}

// Start launches the ticker loop in a background goroutine.
func (j *MonthlyUsageResetJob) Start(ctx context.Context) {
	if j == nil || j.premium == nil {
		slog.Warn("monthly_usage_reset_job: not started — premium service missing")
		return
	}
	slog.Info("monthly_usage_reset_job: started",
		"check_every", j.checkEvery.String(),
		"timezone", "UTC",
		"persistent_lock", j.locks != nil,
	)
	go j.loop(ctx)
}

func (j *MonthlyUsageResetJob) loop(ctx context.Context) {
	j.maybeRun(ctx)

	ticker := time.NewTicker(j.checkEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("monthly_usage_reset_job: stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			j.maybeRun(ctx)
		}
	}
}

func (j *MonthlyUsageResetJob) maybeRun(ctx context.Context) {
	now := time.Now().UTC()
	monthKey := now.Format("2006-01")

	// Only run on the 1st UTC (any hour after midnight — hourly ticker will hit it).
	if now.Day() != 1 {
		slog.Debug("monthly_usage_reset_job: waiting for 1st UTC",
			"now", now.Format(time.RFC3339),
			"day", now.Day(),
		)
		return
	}

	j.mu.Lock()
	already := j.lastRunMonth == monthKey
	j.mu.Unlock()
	if already {
		return
	}

	if j.locks != nil {
		claimed, err := j.locks.TryClaim(ctx, monthlyUsageResetJobName, monthKey)
		if err != nil {
			slog.Error("monthly_usage_reset_job: lock claim failed", "error", err)
			return
		}
		if !claimed {
			slog.Info("monthly_usage_reset_job: skipped — another replica claimed",
				"month", monthKey,
			)
			j.mu.Lock()
			j.lastRunMonth = monthKey
			j.mu.Unlock()
			return
		}
	}

	slog.Info("monthly_usage_reset_job: running", "month", monthKey)
	deleted, err := j.premium.ResetMonthlyUsage(ctx)
	if err != nil {
		slog.Error("monthly_usage_reset_job: reset failed",
			"month", monthKey,
			"error", err,
		)
		if j.locks != nil {
			_ = j.locks.ReleaseClaim(ctx, monthlyUsageResetJobName, monthKey)
		}
		return
	}

	j.mu.Lock()
	j.lastRunMonth = monthKey
	j.mu.Unlock()

	slog.Info("monthly_usage_reset_job: complete",
		"month", monthKey,
		"deleted_rows", deleted,
	)
}
