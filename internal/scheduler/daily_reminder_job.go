// Package scheduler hosts long-running background jobs for the API process.
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
	pushuc "github.com/dadiary/backend/internal/usecase/push"
)

// Default check cadence — frequent enough to hit the configured minute window
// without waking the process every few seconds.
const dailyReminderCheckInterval = 30 * time.Minute

// JobLockStore persists once-per-day claims across API replicas / restarts.
// Implemented by repository.GormPushJobLockRepository.
type JobLockStore interface {
	TryClaim(ctx context.Context, jobName, runDate string) (bool, error)
	ReleaseClaim(ctx context.Context, jobName, runDate string) error
}

// DailyReminderJob wakes once per Vietnam civil day after the configured
// Asia/Ho_Chi_Minh clock time and fans out evening push notifications:
//  1. daily_reminder — not checked in today AND not streak-at-risk
//  2. streak_at_risk — EvaluateStreakView IsAtRisk (days_since 1 or savable 2)
//
// "Today" matches streaktime (same calendar as SkinCheck / streak rows).
// Claims are persisted via JobLockStore when configured (multi-replica safe);
// in-memory last*RunDate remains a fast local guard within one process.
type DailyReminderJob struct {
	push   *pushuc.Service
	locks  JobLockStore
	hour   int
	minute int

	mu                sync.Mutex
	lastDailyRunDate  string // "2006-01-02" Vietnam civil date (process-local)
	lastStreakRunDate string
	loc               *time.Location
	checkEvery        time.Duration
}

// NewDailyReminderJob wires the job from app config.
// locks may be nil (falls back to in-memory only — single-process Beta).
func NewDailyReminderJob(
	push *pushuc.Service,
	cfg *config.Config,
	locks JobLockStore,
) *DailyReminderJob {
	hour, minute := 20, 0
	if cfg != nil {
		hour = cfg.DailyReminder.Hour
		minute = cfg.DailyReminder.Minute
	}
	return &DailyReminderJob{
		push:       push,
		locks:      locks,
		hour:       hour,
		minute:     minute,
		loc:        streaktime.Location,
		checkEvery: dailyReminderCheckInterval,
	}
}

// Start launches the ticker loop in a background goroutine.
// Cancel ctx (e.g. SIGINT/SIGTERM in main) to stop cleanly.
func (j *DailyReminderJob) Start(ctx context.Context) {
	if j == nil || j.push == nil {
		slog.Warn("daily_reminder_job: not started — push service missing")
		return
	}
	slog.Info("daily_reminder_job: started",
		"hour", j.hour,
		"minute", j.minute,
		"check_every", j.checkEvery.String(),
		"timezone", j.loc.String(),
		"persistent_lock", j.locks != nil,
	)
	go j.loop(ctx)
}

func (j *DailyReminderJob) loop(ctx context.Context) {
	// Immediate check so a deploy/restart shortly after the target time still fires once.
	j.maybeRun(ctx)

	ticker := time.NewTicker(j.checkEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("daily_reminder_job: stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			j.maybeRun(ctx)
		}
	}
}

func (j *DailyReminderJob) maybeRun(ctx context.Context) {
	now := streaktime.Now()
	today := streaktime.TodayString()

	if !shouldRunDailyReminder(now, j.hour, j.minute) {
		slog.Debug("daily_reminder_job: waiting for window",
			"now", now.Format(time.RFC3339),
			"target", formatHHMM(j.hour, j.minute),
			"timezone", j.loc.String(),
		)
		return
	}

	j.mu.Lock()
	needDaily := j.lastDailyRunDate != today
	needStreak := j.lastStreakRunDate != today
	j.mu.Unlock()

	if !needDaily && !needStreak {
		slog.Debug("daily_reminder_job: already ran today (memory)", "date", today)
		return
	}

	if needDaily {
		j.runDailyReminders(ctx, today)
	}
	if needStreak {
		j.runStreakAtRisk(ctx, today)
	}
}

func (j *DailyReminderJob) runDailyReminders(ctx context.Context, today string) {
	if !j.claim(ctx, domain.PushJobDailyReminder, today, &j.lastDailyRunDate) {
		return
	}

	slog.Info("Daily Reminder Job started",
		"date", today,
		"target", formatHHMM(j.hour, j.minute),
	)

	result, err := j.push.SendDailyRemindersToAll(ctx)
	if err != nil {
		slog.Error("Daily Reminder Job finished with error",
			"date", today,
			"total", result.Total,
			"sent", result.Sent,
			"skipped", result.Skipped,
			"skipped_already_checked_in", result.SkippedAlreadyCheckedIn,
			"skipped_at_risk", result.SkippedAtRisk,
			"skipped_no_subscription", result.SkippedNoSubscription,
			"failed", result.Failed,
			"err", err,
		)
	} else {
		slog.Info("Daily Reminder Job finished",
			"date", today,
			"total", result.Total,
			"sent", result.Sent,
			"skipped", result.Skipped,
			"skipped_already_checked_in", result.SkippedAlreadyCheckedIn,
			"skipped_at_risk", result.SkippedAtRisk,
			"skipped_no_subscription", result.SkippedNoSubscription,
			"failed", result.Failed,
		)
	}
	if shouldUnclaimPushBatch(result.Sent, result.Failed, err) {
		j.release(ctx, domain.PushJobDailyReminder, today, &j.lastDailyRunDate)
		slog.Info("Daily Reminder Job unclaimed for retry", "date", today)
	}
}

func (j *DailyReminderJob) runStreakAtRisk(ctx context.Context, today string) {
	if !j.claim(ctx, domain.PushJobStreakAtRisk, today, &j.lastStreakRunDate) {
		return
	}

	slog.Info("Streak At Risk notifications started", "date", today)
	result, err := j.push.SendStreakAtRiskNotifications(ctx)
	if err != nil {
		slog.Error("Streak At Risk notifications finished with error",
			"date", today,
			"total", result.Total,
			"sent", result.Sent,
			"skipped", result.Skipped,
			"skipped_already_notified", result.SkippedAlreadyNotified,
			"skipped_already_checked_in", result.SkippedAlreadyCheckedIn,
			"skipped_no_longer_relevant", result.SkippedNoLongerRelevant,
			"skipped_no_subscription", result.SkippedNoSubscription,
			"failed", result.Failed,
			"err", err,
		)
	} else {
		slog.Info("Streak At Risk notifications finished",
			"date", today,
			"total", result.Total,
			"sent", result.Sent,
			"skipped", result.Skipped,
			"skipped_already_notified", result.SkippedAlreadyNotified,
			"skipped_already_checked_in", result.SkippedAlreadyCheckedIn,
			"skipped_no_longer_relevant", result.SkippedNoLongerRelevant,
			"skipped_no_subscription", result.SkippedNoSubscription,
			"failed", result.Failed,
		)
	}
	if shouldUnclaimPushBatch(result.Sent, result.Failed, err) {
		j.release(ctx, domain.PushJobStreakAtRisk, today, &j.lastStreakRunDate)
		slog.Info("Streak At Risk notifications unclaimed for retry", "date", today)
	}
}

// claim takes the process-local + durable lock. Returns false if already claimed today.
func (j *DailyReminderJob) claim(
	ctx context.Context,
	jobName, today string,
	memDate *string,
) bool {
	j.mu.Lock()
	if *memDate == today {
		j.mu.Unlock()
		return false
	}
	// Tentative local claim: blocks overlapping ticks in this process while
	// TryClaim is in flight. Cleared below if we do not win the durable lock.
	*memDate = today
	j.mu.Unlock()

	if j.locks == nil {
		return true
	}
	ok, err := j.locks.TryClaim(ctx, jobName, today)
	if err != nil {
		slog.Error("push_job_lock: TryClaim failed — aborting run (no fan-out without lock)",
			"job", jobName,
			"date", today,
			"err", err,
		)
		j.clearMemDate(memDate, today)
		return false
	}
	if !ok {
		slog.Info("push_job_lock: already claimed today",
			"job", jobName,
			"date", today,
		)
		// Another pod holds today's claim. Clear tentative memDate so this
		// process can TryClaim again on later ticks if they ReleaseClaim
		// (setup failure / unclaim). Sticking memDate here used to skip the
		// whole evening after a peer released and died/redeployed.
		j.clearMemDate(memDate, today)
		return false
	}
	// Won durable claim — keep memDate = today for the rest of the VN day
	// (or until release() clears it on retryable failure).
	return true
}

func (j *DailyReminderJob) clearMemDate(memDate *string, today string) {
	j.mu.Lock()
	if *memDate == today {
		*memDate = ""
	}
	j.mu.Unlock()
}

// pushJobReleaseTimeout bounds DB unclaim after shutdown cancel so we do not
// hang the process, but still outlive the canceled parent fan-out ctx.
const pushJobReleaseTimeout = 5 * time.Second

func (j *DailyReminderJob) release(
	ctx context.Context,
	jobName, today string,
	memDate *string,
) {
	j.mu.Lock()
	if *memDate == today {
		*memDate = ""
	}
	j.mu.Unlock()

	if j.locks == nil {
		return
	}
	// Parent ctx is often already canceled (SIGTERM / deploy mid-fan-out).
	// Releasing with that ctx fails → claim stuck all evening on every replica.
	// Detach cancel and use a short timeout so unclaim still reaches Postgres.
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pushJobReleaseTimeout)
	defer cancel()
	if err := j.locks.ReleaseClaim(releaseCtx, jobName, today); err != nil {
		slog.Error("push_job_lock: ReleaseClaim failed",
			"job", jobName,
			"date", today,
			"err", err,
		)
	}
}

// shouldUnclaimPushBatch reports whether today's claim should be released so
// a later ticker (or another replica) can retry.
//
// Durable push_send_receipts skip users already delivered, so we can unclaim
// after partial success and retry only failures / remaining users:
//   - runErr != nil → unclaim (cancel / setup fail — receipts protect sent)
//   - failed > 0 → unclaim (retry provider failures)
//   - otherwise keep claim (clean finish or all skipped)
func shouldUnclaimPushBatch(sent, failed int, runErr error) bool {
	_ = sent
	if runErr != nil {
		return true
	}
	return failed > 0
}

// shouldRunDailyReminder reports whether now (already in streaktime.Location)
// is on/after today's configured Hour:Minute.
func shouldRunDailyReminder(now time.Time, hour, minute int) bool {
	if now.Hour() > hour {
		return true
	}
	if now.Hour() == hour && now.Minute() >= minute {
		return true
	}
	return false
}

func formatHHMM(hour, minute int) string {
	return time.Date(2000, 1, 1, hour, minute, 0, 0, time.UTC).Format("15:04")
}
