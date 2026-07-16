package push

import (
	"context"
	"errors"
	"log/slog"

	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

// DailyReminderBatchResult summarises a fan-out run of SendDailyRemindersToAll.
type DailyReminderBatchResult struct {
	Total int `json:"total"` // users with an active subscription
	Sent  int `json:"sent"`  // delivered successfully
	// Skipped is the total of all skip reasons (no sub + already checked in + …).
	Skipped int `json:"skipped"`
	// SkippedAlreadyCheckedIn counts users skipped because they checked in today.
	SkippedAlreadyCheckedIn int `json:"skipped_already_checked_in"`
	// SkippedAtRisk counts users skipped because they will receive streak_at_risk instead.
	SkippedAtRisk int `json:"skipped_at_risk"`
	// SkippedNoSubscription counts missing/inactive push subscriptions.
	SkippedNoSubscription int `json:"skipped_no_subscription"`
	Failed                int `json:"failed"` // send error (expired endpoint, network, …)
}

// SendDailyReminderToUser sends one daily_reminder push to a single user.
//
// Skips (ErrAlreadyCheckedIn) when the user already has a SkinCheck for today's
// Vietnam civil date. Expired subscriptions are cleaned up inside PushSender.
//
// Callers that fan out should also exclude at-risk users (see
// SendDailyRemindersToAll) so they only receive streak_at_risk.
func (s *Service) SendDailyReminderToUser(ctx context.Context, userID uuid.UUID) error {
	if s == nil || s.sender == nil || s.repo == nil {
		return ErrSenderUnavailable
	}
	if userID == uuid.Nil {
		return ErrNotFound
	}

	today := streaktime.TodayString()
	nType := string(pushsvc.NotificationTypeDailyReminder)
	if s.alreadySentDurable(ctx, userID, nType, today) {
		slog.Info("daily_reminder: skip — already sent today (receipt)",
			"user_id", userID.String(),
			"date", today,
		)
		return ErrAlreadyNotifiedToday
	}

	if s.checkIns != nil {
		done, err := s.checkIns.HasCheckedInToday(ctx, userID)
		if err != nil {
			slog.Error("daily_reminder: HasCheckedInToday failed",
				"user_id", userID.String(),
				"err", err,
			)
			return err
		}
		if done {
			slog.Info("daily_reminder: skip — already checked in today",
				"user_id", userID.String(),
			)
			return ErrAlreadyCheckedIn
		}
	}

	sub, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		slog.Error("daily_reminder: load subscription failed",
			"user_id", userID.String(),
			"err", err,
		)
		return err
	}
	if sub == nil {
		slog.Info("daily_reminder: skip — no active subscription",
			"user_id", userID.String(),
		)
		return ErrNotFound
	}

	payload := pushsvc.BuildNotificationPayload(pushsvc.NotificationTypeDailyReminder, nil)
	if err := s.sender.SendToSubscription(ctx, sub, payload); err != nil {
		switch {
		case errors.Is(err, pushsvc.ErrNotConfigured):
			slog.Error("daily_reminder: sender not configured",
				"user_id", userID.String(),
			)
			return ErrSenderUnavailable
		case errors.Is(err, pushsvc.ErrSubscriptionExpired):
			slog.Warn("daily_reminder: subscription expired (cleaned up)",
				"user_id", userID.String(),
				"subscription_id", sub.ID.String(),
			)
			return err
		default:
			slog.Error("daily_reminder: send failed",
				"user_id", userID.String(),
				"subscription_id", sub.ID.String(),
				"err", err,
			)
			return err
		}
	}

	s.markSentDurable(ctx, userID, nType, today)
	slog.Info("daily_reminder: sent",
		"user_id", userID.String(),
		"subscription_id", sub.ID.String(),
		"tag", payload.Tag,
	)
	return nil
}

// SendDailyRemindersToAll fans out daily_reminder to every user with an active
// push subscription who has not checked in today and is not streak-at-risk
// (including days_since == 2 savable cases from ListUsersAtRisk). At-risk users
// are skipped so streak_at_risk is their only evening nudge.
func (s *Service) SendDailyRemindersToAll(ctx context.Context) (DailyReminderBatchResult, error) {
	var result DailyReminderBatchResult
	if s == nil || s.sender == nil || s.repo == nil {
		return result, ErrSenderUnavailable
	}

	userIDs, err := s.repo.ListActiveUserIDs(ctx)
	if err != nil {
		slog.Error("daily_reminder: list active users failed", "err", err)
		return result, err
	}

	// Prefetch at-risk BEFORE setting Total. If this fails, Total stays 0 so the
	// scheduler can unclaim today's run and retry (avoids losing the whole day).
	atRisk, err := s.loadAtRiskPushSet(ctx)
	if err != nil {
		return result, err
	}

	result.Total = len(userIDs)

	slog.Info("daily_reminder: fan-out starting",
		"total", result.Total,
		"at_risk_excluded", len(atRisk),
	)

	for _, userID := range userIDs {
		if err := ctx.Err(); err != nil {
			slog.Warn("daily_reminder: fan-out canceled",
				"sent", result.Sent,
				"failed", result.Failed,
				"skipped", result.Skipped,
				"skipped_already_checked_in", result.SkippedAlreadyCheckedIn,
				"skipped_at_risk", result.SkippedAtRisk,
				"err", err,
			)
			return result, err
		}

		var sendErr error
		if _, skip := atRisk[userID]; skip {
			slog.Info("daily_reminder: skip — user at risk (streak_at_risk will notify)",
				"user_id", userID.String(),
			)
			sendErr = ErrAtRisk
		} else {
			sendErr = s.SendDailyReminderToUser(ctx, userID)
		}

		switch {
		case sendErr == nil:
			result.Sent++
		case errors.Is(sendErr, ErrAtRisk):
			result.Skipped++
			result.SkippedAtRisk++
		case errors.Is(sendErr, ErrAlreadyCheckedIn):
			result.Skipped++
			result.SkippedAlreadyCheckedIn++
		case errors.Is(sendErr, ErrAlreadyNotifiedToday):
			result.Skipped++ // durable receipt — safe on job retry
		case errors.Is(sendErr, ErrNotFound):
			result.Skipped++
			result.SkippedNoSubscription++
		case errors.Is(sendErr, ErrSenderUnavailable):
			result.Skipped++
		default:
			result.Failed++
			// Continue — one bad endpoint must not abort the whole batch.
		}
	}

	slog.Info("daily_reminder: fan-out finished",
		"total", result.Total,
		"sent", result.Sent,
		"skipped", result.Skipped,
		"skipped_already_checked_in", result.SkippedAlreadyCheckedIn,
		"skipped_at_risk", result.SkippedAtRisk,
		"skipped_no_subscription", result.SkippedNoSubscription,
		"failed", result.Failed,
	)
	return result, nil
}

// loadAtRiskPushSet returns user IDs that should receive streak_at_risk tonight
// instead of daily_reminder. Empty map when streak source is unavailable.
func (s *Service) loadAtRiskPushSet(ctx context.Context) (map[uuid.UUID]struct{}, error) {
	out := make(map[uuid.UUID]struct{})
	if s == nil || s.streaks == nil {
		slog.Warn("daily_reminder: streak source missing — cannot exclude at-risk users")
		return out, nil
	}

	ids, err := s.GetUsersAtRiskWithPush(ctx)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			slog.Warn("daily_reminder: GetUsersAtRiskWithPush unavailable — cannot exclude at-risk users")
			return out, nil
		}
		slog.Error("daily_reminder: GetUsersAtRiskWithPush failed", "err", err)
		return nil, err
	}
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
}
