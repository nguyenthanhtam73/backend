package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	subscriptionuc "github.com/dadiary/backend/internal/usecase/subscription"
	"github.com/dadiary/backend/pkg/alert"
)

const (
	planExpiryJobName    = "plan_expiry_downgrade"
	planExpiryCheckEvery = 1 * time.Hour
	// downgradeSpikeThreshold alerts when a single run downgrades more than this many users.
	downgradeSpikeThreshold = 20
)

// PlanExpiryJob runs once per UTC day and downgrades users whose grace window
// has ended (plan_expires_at + grace_days). Feature gates already treat
// post-grace rows as Free via EffectivePlanTierWithGrace; this job keeps
// plan_tier / subscription_status in the DB consistent.
type PlanExpiryJob struct {
	premium *premiumuc.Service
	subs    *subscriptionuc.Service
	locks   JobLockStore
	alerter alert.Alerter

	mu         sync.Mutex
	lastRunDay string // "2006-01-02" UTC
	checkEvery time.Duration
}

// NewPlanExpiryJob wires the daily grace/expiry cron.
// Prefer subs (writes subscriptions history); premium is the fallback.
// locks / alerter may be nil.
func NewPlanExpiryJob(
	premium *premiumuc.Service,
	subs *subscriptionuc.Service,
	locks JobLockStore,
) *PlanExpiryJob {
	return &PlanExpiryJob{
		premium:    premium,
		subs:       subs,
		locks:      locks,
		checkEvery: planExpiryCheckEvery,
	}
}

// AttachAlerter wires ops alerts for job failure / downgrade spikes.
func (j *PlanExpiryJob) AttachAlerter(a alert.Alerter) {
	if j == nil {
		return
	}
	j.alerter = a
}

// Start launches the ticker loop in a background goroutine.
func (j *PlanExpiryJob) Start(ctx context.Context) {
	if j == nil || (j.subs == nil && j.premium == nil) {
		slog.Warn("plan_expiry_job: not started — subscription/premium service missing")
		return
	}
	slog.Info("plan_expiry_job: started",
		"check_every", j.checkEvery.String(),
		"timezone", "UTC",
		"persistent_lock", j.locks != nil,
		"lifecycle", j.subs != nil,
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
			slog.Error("plan_expiry_job: lock claim failed",
				"day", dayKey,
				"error", err.Error(),
			)
			j.alertJob(ctx, "lock_claim_failed", err.Error(), map[string]any{
				"day": dayKey,
			})
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

	started := time.Now().UTC()
	slog.Info("plan_expiry_job: start",
		"day", dayKey,
		"started_at", started.Format(time.RFC3339),
	)

	n, err := j.runDowngrade(ctx)
	elapsed := time.Since(started)

	if err != nil {
		slog.Error("plan_expiry_job: fail",
			"day", dayKey,
			"downgraded", n,
			"elapsed_ms", elapsed.Milliseconds(),
			"error", err.Error(),
		)
		j.alertJob(ctx, "job_failed", err.Error(), map[string]any{
			"day":         dayKey,
			"downgraded":  n,
			"elapsed_ms":  elapsed.Milliseconds(),
		})
		if j.locks != nil {
			_ = j.locks.ReleaseClaim(ctx, planExpiryJobName, dayKey)
		}
		return
	}

	j.mu.Lock()
	j.lastRunDay = dayKey
	j.mu.Unlock()

	slog.Info("plan_expiry_job: end",
		"day", dayKey,
		"success_count", n,
		"elapsed_ms", elapsed.Milliseconds(),
		"status", "ok",
	)

	if n > downgradeSpikeThreshold {
		slog.Error("plan_expiry_job: downgrade spike",
			"day", dayKey,
			"downgraded", n,
			"threshold", downgradeSpikeThreshold,
		)
		j.alertJob(ctx, "downgrade_spike", "downgraded more users than threshold", map[string]any{
			"day":         dayKey,
			"downgraded":  n,
			"threshold":   downgradeSpikeThreshold,
			"elapsed_ms":  elapsed.Milliseconds(),
		})
	}
}

func (j *PlanExpiryJob) runDowngrade(ctx context.Context) (int, error) {
	if j.subs != nil {
		return j.subs.DowngradePastGrace(ctx)
	}
	return j.premium.DowngradeExpiredPlans(ctx)
}

func (j *PlanExpiryJob) alertJob(ctx context.Context, reason, message string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["reason"] = reason
	key := reason
	switch reason {
	case "job_failed":
		key = alert.KeyJobFailed
	case "downgrade_spike":
		key = alert.KeyDowngradeSpike
	case "lock_claim_failed":
		key = alert.KeyLockClaimFailed
	}
	// Fanout: 15m cooldown per key + detached Slack/Telegram (non-blocking).
	alert.Send(ctx, j.alerter, alert.Event{
		Key:     key,
		Title:   "PlanExpiryJob: " + reason,
		Level:   alert.LevelError,
		Message: message,
		Fields:  fields,
	})
}
