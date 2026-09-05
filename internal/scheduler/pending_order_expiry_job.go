package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	paymentuc "github.com/dadiary/backend/internal/usecase/payment"
)

const (
	pendingOrderExpiryJobName    = "pending_order_expiry"
	pendingOrderExpiryCheckEvery = 1 * time.Hour
)

// PendingOrderExpiryJob marks leftover SePay payment_orders pending past the
// local TTL as expired. Same lock style as PlanExpiryJob (once per UTC day).
type PendingOrderExpiryJob struct {
	pay   *paymentuc.Service
	locks JobLockStore

	mu         sync.Mutex
	lastRunDay string
	checkEvery time.Duration
}

// NewPendingOrderExpiryJob wires the daily hygiene cron. locks may be nil.
func NewPendingOrderExpiryJob(pay *paymentuc.Service, locks JobLockStore) *PendingOrderExpiryJob {
	return &PendingOrderExpiryJob{
		pay:        pay,
		locks:      locks,
		checkEvery: pendingOrderExpiryCheckEvery,
	}
}

// Start launches the ticker loop in a background goroutine.
func (j *PendingOrderExpiryJob) Start(ctx context.Context) {
	if j == nil || j.pay == nil {
		slog.Warn("pending_order_expiry_job: not started — payment service missing")
		return
	}
	slog.Info("pending_order_expiry_job: started",
		"check_every", j.checkEvery.String(),
		"timezone", "UTC",
		"persistent_lock", j.locks != nil,
	)
	go j.loop(ctx)
}

func (j *PendingOrderExpiryJob) loop(ctx context.Context) {
	j.maybeRun(ctx)

	ticker := time.NewTicker(j.checkEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("pending_order_expiry_job: stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			j.maybeRun(ctx)
		}
	}
}

func (j *PendingOrderExpiryJob) maybeRun(ctx context.Context) {
	now := time.Now().UTC()
	dayKey := now.Format("2006-01-02")

	j.mu.Lock()
	already := j.lastRunDay == dayKey
	j.mu.Unlock()
	if already {
		return
	}

	if j.locks != nil {
		claimed, err := j.locks.TryClaim(ctx, pendingOrderExpiryJobName, dayKey)
		if err != nil {
			slog.Error("pending_order_expiry_job: lock claim failed",
				"day", dayKey,
				"error", err.Error(),
			)
			return
		}
		if !claimed {
			slog.Info("pending_order_expiry_job: skipped — another replica claimed", "day", dayKey)
			j.mu.Lock()
			j.lastRunDay = dayKey
			j.mu.Unlock()
			return
		}
	}

	started := time.Now().UTC()
	slog.Info("pending_order_expiry_job: start", "day", dayKey)

	res, err := j.pay.ExpireStalePending(ctx, now)
	elapsed := time.Since(started)
	if err != nil {
		slog.Error("pending_order_expiry_job: fail",
			"day", dayKey,
			"elapsed_ms", elapsed.Milliseconds(),
			"error", err.Error(),
		)
		if j.locks != nil {
			_ = j.locks.ReleaseClaim(ctx, pendingOrderExpiryJobName, dayKey)
		}
		return
	}

	j.mu.Lock()
	j.lastRunDay = dayKey
	j.mu.Unlock()

	slog.Info("pending_order_expiry_job: end",
		"day", dayKey,
		"expired", res.Expired,
		"pending_fresh", res.PendingFresh,
		"ttl_hours", res.TTLHours,
		"elapsed_ms", elapsed.Milliseconds(),
	)
}
