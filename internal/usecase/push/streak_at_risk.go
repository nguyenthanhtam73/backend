package push

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/dadiary/backend/internal/dto"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

// ErrAlreadyNotifiedToday means this user already received streak_at_risk today
// (Vietnam civil date). Prevents double-send on job retry / overlapping calls.
var ErrAlreadyNotifiedToday = errors.New("streak_at_risk already sent today")

// StreakAtRiskBatchResult summarises a fan-out run of SendStreakAtRiskNotifications.
type StreakAtRiskBatchResult struct {
	Total   int `json:"total"`   // at-risk users with an active push subscription
	Sent    int `json:"sent"`    // delivered successfully
	Skipped int `json:"skipped"` // no sub / already notified / no longer relevant / …
	// SkippedAlreadyNotified counts users skipped by the once-per-day guard.
	SkippedAlreadyNotified int `json:"skipped_already_notified"`
	// SkippedAlreadyCheckedIn counts users who checked in after the candidate list.
	SkippedAlreadyCheckedIn int `json:"skipped_already_checked_in"`
	// SkippedNoLongerRelevant counts users no longer IsAtRisk at send time.
	SkippedNoLongerRelevant int `json:"skipped_no_longer_relevant"`
	// SkippedNoSubscription counts missing/inactive push subscriptions.
	SkippedNoSubscription int `json:"skipped_no_subscription"`
	Failed                int `json:"failed"` // send error (expired endpoint, network, …)
}

// streakAtRiskDedupe remembers successful streak_at_risk sends for the current
// Vietnam civil day. In-memory only (same trade-off as DailyReminderJob.lastRunDate):
// a process restart may notify again the same day — acceptable for Beta.
type streakAtRiskDedupe struct {
	mu    sync.Mutex
	date  string // "2006-01-02" VN
	users map[uuid.UUID]struct{}
}

func (d *streakAtRiskDedupe) alreadySent(userID uuid.UUID, today string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.date != today {
		return false
	}
	_, ok := d.users[userID]
	return ok
}

func (d *streakAtRiskDedupe) markSent(userID uuid.UUID, today string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.date != today {
		d.date = today
		d.users = make(map[uuid.UUID]struct{})
	}
	if d.users == nil {
		d.users = make(map[uuid.UUID]struct{})
	}
	d.users[userID] = struct{}{}
}

// GetUsersAtRiskWithPush returns user IDs that are both:
//  1. streak at risk today (ListUsersAtRisk ≈ EvaluateStreakView IsAtRisk,
//     including days_since == 1 and savable days_since == 2), and
//  2. have an active Web Push subscription.
func (s *Service) GetUsersAtRiskWithPush(ctx context.Context) ([]uuid.UUID, error) {
	if s == nil || s.repo == nil || s.streaks == nil {
		return nil, ErrUnavailable
	}

	atRisk, err := s.streaks.ListUsersAtRisk(ctx)
	if err != nil {
		slog.Error("streak_at_risk: ListUsersAtRisk failed", "err", err)
		return nil, err
	}
	if len(atRisk) == 0 {
		slog.Info("streak_at_risk: no users at risk today")
		return nil, nil
	}

	withPush, err := s.repo.ListActiveUserIDs(ctx)
	if err != nil {
		slog.Error("streak_at_risk: ListActiveUserIDs failed", "err", err)
		return nil, err
	}
	if len(withPush) == 0 {
		slog.Info("streak_at_risk: no active push subscriptions",
			"at_risk_total", len(atRisk),
		)
		return nil, nil
	}

	pushSet := make(map[uuid.UUID]struct{}, len(withPush))
	for _, id := range withPush {
		pushSet[id] = struct{}{}
	}

	out := make([]uuid.UUID, 0, len(atRisk))
	for _, id := range atRisk {
		if _, ok := pushSet[id]; ok {
			out = append(out, id)
		}
	}

	slog.Info("streak_at_risk: candidates ready",
		"at_risk_total", len(atRisk),
		"with_push", len(out),
	)
	return out, nil
}

// ensureStillRelevantForStreakAtRisk re-checks immediately before send so a long
// fan-out (or delayed delivery after listing) does not nudge users who already
// checked in or left the IsAtRisk window (TTL / race).
func (s *Service) ensureStillRelevantForStreakAtRisk(
	ctx context.Context,
	userID uuid.UUID,
) error {
	if s.checkIns != nil {
		done, err := s.checkIns.HasCheckedInToday(ctx, userID)
		if err != nil {
			slog.Error("streak_at_risk: HasCheckedInToday failed",
				"user_id", userID.String(),
				"err", err,
			)
			return err
		}
		if done {
			slog.Info("streak_at_risk: skip — already checked in today",
				"user_id", userID.String(),
			)
			return ErrAlreadyCheckedIn
		}
	}

	if s.streaks == nil {
		return nil
	}
	row, err := s.streaks.GetByUserID(ctx, userID)
	if err != nil {
		slog.Error("streak_at_risk: GetByUserID failed",
			"user_id", userID.String(),
			"err", err,
		)
		return err
	}
	view := dto.EvaluateStreakView(row, streaktime.Today())
	if !view.IsAtRisk {
		slog.Info("streak_at_risk: skip — no longer at risk",
			"user_id", userID.String(),
		)
		return ErrNoLongerAtRisk
	}
	return nil
}

// SendStreakAtRiskToUser sends one streak_at_risk push to a single user.
//
// Skips when already notified today, already checked in, or no longer at risk.
// Expired subscriptions are cleaned up inside PushSender (404/410).
func (s *Service) SendStreakAtRiskToUser(ctx context.Context, userID uuid.UUID) error {
	if s == nil || s.sender == nil || s.repo == nil {
		return ErrSenderUnavailable
	}
	if userID == uuid.Nil {
		return ErrNotFound
	}

	today := streaktime.TodayString()
	nType := string(pushsvc.NotificationTypeStreakAtRisk)
	if s.streakAtRiskSent.alreadySent(userID, today) ||
		s.alreadySentDurable(ctx, userID, nType, today) {
		slog.Info("streak_at_risk: skip — already notified today",
			"user_id", userID.String(),
			"date", today,
		)
		return ErrAlreadyNotifiedToday
	}

	if err := s.ensureStillRelevantForStreakAtRisk(ctx, userID); err != nil {
		return err
	}

	if err := s.SendByType(ctx, userID, pushsvc.NotificationTypeStreakAtRisk, nil); err != nil {
		switch {
		case errors.Is(err, ErrSenderUnavailable):
			slog.Error("streak_at_risk: sender not configured",
				"user_id", userID.String(),
			)
			return err
		case errors.Is(err, ErrNotFound):
			slog.Info("streak_at_risk: skip — no active subscription",
				"user_id", userID.String(),
			)
			return err
		case errors.Is(err, pushsvc.ErrSubscriptionExpired):
			slog.Warn("streak_at_risk: subscription expired (cleaned up)",
				"user_id", userID.String(),
			)
			return err
		default:
			slog.Error("streak_at_risk: send failed",
				"user_id", userID.String(),
				"err", err,
			)
			return err
		}
	}

	s.streakAtRiskSent.markSent(userID, today)
	s.markSentDurable(ctx, userID, nType, today)
	slog.Info("streak_at_risk: sent",
		"user_id", userID.String(),
		"date", today,
	)
	return nil
}

// SendStreakAtRiskNotifications fans out streak_at_risk to every at-risk user
// with an active push subscription. Sequential so ctx cancellation and
// push-provider rate limits stay predictable.
func (s *Service) SendStreakAtRiskNotifications(ctx context.Context) (StreakAtRiskBatchResult, error) {
	var result StreakAtRiskBatchResult
	if s == nil || s.sender == nil || s.repo == nil || s.streaks == nil {
		return result, ErrSenderUnavailable
	}

	userIDs, err := s.GetUsersAtRiskWithPush(ctx)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return result, ErrSenderUnavailable
		}
		return result, err
	}
	result.Total = len(userIDs)

	slog.Info("streak_at_risk: fan-out starting", "total", result.Total)

	for _, userID := range userIDs {
		if err := ctx.Err(); err != nil {
			slog.Warn("streak_at_risk: fan-out canceled",
				"sent", result.Sent,
				"failed", result.Failed,
				"skipped", result.Skipped,
				"err", err,
			)
			return result, err
		}

		err := s.SendStreakAtRiskToUser(ctx, userID)
		switch {
		case err == nil:
			result.Sent++
		case errors.Is(err, ErrAlreadyNotifiedToday):
			result.Skipped++
			result.SkippedAlreadyNotified++
		case errors.Is(err, ErrAlreadyCheckedIn):
			result.Skipped++
			result.SkippedAlreadyCheckedIn++
		case errors.Is(err, ErrNoLongerAtRisk):
			result.Skipped++
			result.SkippedNoLongerRelevant++
		case errors.Is(err, ErrNotFound):
			result.Skipped++
			result.SkippedNoSubscription++
		case errors.Is(err, ErrSenderUnavailable):
			result.Skipped++
		default:
			result.Failed++
			// Continue — one bad endpoint must not abort the whole batch.
		}
	}

	slog.Info("streak_at_risk: fan-out finished",
		"total", result.Total,
		"sent", result.Sent,
		"skipped", result.Skipped,
		"skipped_already_notified", result.SkippedAlreadyNotified,
		"skipped_already_checked_in", result.SkippedAlreadyCheckedIn,
		"skipped_no_longer_relevant", result.SkippedNoLongerRelevant,
		"skipped_no_subscription", result.SkippedNoSubscription,
		"failed", result.Failed,
	)
	return result, nil
}
