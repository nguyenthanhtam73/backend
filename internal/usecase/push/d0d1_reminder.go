package push

import (
	"context"
	"errors"
	"log/slog"

	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

// SendD0D1ReminderToUser sends one typed d0_reminder / d1_reminder push.
//
// Skips when already checked in, no subscription, or any of daily_reminder /
// d0_reminder / d1_reminder already has a receipt for this Vietnam civil day.
func (s *Service) SendD0D1ReminderToUser(ctx context.Context, userID uuid.UUID, kind string) error {
	if s == nil || s.sender == nil || s.repo == nil {
		return ErrSenderUnavailable
	}
	if userID == uuid.Nil {
		return ErrNotFound
	}

	nType, ok := d0d1NotificationType(kind)
	if !ok {
		return ErrNotFound
	}

	today := streaktime.TodayString()
	if s.alreadySentAnyDurable(ctx, userID, today, pushsvc.ReminderTypesSameVNDay...) {
		slog.Info("d0_d1_reminder: skip — already nudged today (receipt)",
			"user_id", userID.String(),
			"kind", kind,
			"date", today,
		)
		return ErrAlreadyNotifiedToday
	}

	if s.checkIns != nil {
		done, err := s.checkIns.HasCheckedInToday(ctx, userID)
		if err != nil {
			slog.Error("d0_d1_reminder: HasCheckedInToday failed",
				"user_id", userID.String(),
				"err", err,
			)
			return err
		}
		if done {
			slog.Info("d0_d1_reminder: skip — already checked in today",
				"user_id", userID.String(),
				"kind", kind,
			)
			return ErrAlreadyCheckedIn
		}
	}

	if err := s.SendByType(ctx, userID, nType, nil); err != nil {
		switch {
		case errors.Is(err, ErrSenderUnavailable):
			slog.Error("d0_d1_reminder: sender not configured",
				"user_id", userID.String(),
			)
			return err
		case errors.Is(err, ErrNotFound):
			slog.Info("d0_d1_reminder: skip — no active subscription",
				"user_id", userID.String(),
			)
			return err
		case errors.Is(err, pushsvc.ErrSubscriptionExpired):
			slog.Warn("d0_d1_reminder: subscription expired (cleaned up)",
				"user_id", userID.String(),
			)
			return err
		default:
			slog.Error("d0_d1_reminder: send failed",
				"user_id", userID.String(),
				"kind", kind,
				"err", err,
			)
			return err
		}
	}

	s.markSentDurable(ctx, userID, string(nType), today)
	slog.Info("d0_d1_reminder: sent",
		"user_id", userID.String(),
		"kind", kind,
		"type", string(nType),
		"date", today,
	)
	return nil
}

func d0d1NotificationType(kind string) (pushsvc.NotificationType, bool) {
	switch kind {
	case "d0":
		return pushsvc.NotificationTypeD0Reminder, true
	case "d1":
		return pushsvc.NotificationTypeD1Reminder, true
	default:
		return "", false
	}
}
