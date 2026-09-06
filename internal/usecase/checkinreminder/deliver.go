package checkinreminder

import (
	"context"
	"errors"
	"log/slog"
	"net/mail"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/service/email"
	pushuc "github.com/dadiary/backend/internal/usecase/push"
	"github.com/google/uuid"
)

// DeliveryResult summarises one D0/D1 email + push fan-out.
type DeliveryResult struct {
	EmailSent    int `json:"email_sent"`
	EmailSkipped int `json:"email_skipped"`
	EmailFailed  int `json:"email_failed"`
	PushSent     int `json:"push_sent"`
	PushSkipped  int `json:"push_skipped"`
	PushFailed   int `json:"push_failed"`
	Candidates   int `json:"candidates"`
}

func (r DeliveryResult) toDTO() dto.CheckInReminderDeliveryStats {
	return dto.CheckInReminderDeliveryStats{
		EmailSent:    r.EmailSent,
		EmailSkipped: r.EmailSkipped,
		EmailFailed:  r.EmailFailed,
		PushSent:     r.PushSent,
		PushSkipped:  r.PushSkipped,
		PushFailed:   r.PushFailed,
		Candidates:   r.Candidates,
	}
}

// DeliverDue sends ≤1 D0 and ≤1 D1 email per user, plus typed D0/D1 pushes,
// for currently due flags. Idempotent via receipts. ESP-missing is a logged no-op.
func (s *Service) DeliverDue(ctx context.Context) (DeliveryResult, error) {
	var out DeliveryResult
	if err := s.ready(); err != nil {
		return out, err
	}

	rows, err := s.flags.ListDue(ctx, 2000)
	if err != nil {
		return out, err
	}
	out.Candidates = len(rows)

	emailReady := s.mailer != nil && s.mailer.Configured() && s.emailReceipts != nil
	if !emailReady {
		slog.Info("checkin_reminder: email no-op — ESP not configured (set RESEND_API_KEY and EMAIL_FROM)")
	}
	pushReady := s.push != nil && s.jobEnabled && s.vapidConfigured
	if s.jobEnabled && !s.vapidConfigured {
		slog.Info("checkin_reminder: d0/d1 push skipped — VAPID keys not configured")
	}

	slog.Info("checkin_reminder: deliver start",
		"candidates", out.Candidates,
		"email_ready", emailReady,
		"push_ready", pushReady,
	)

	for i := range rows {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		flag := rows[i]
		kind := NormalizeKind(flag.Kind)
		if kind != KindD0 && kind != KindD1 {
			out.EmailSkipped++
			out.PushSkipped++
			continue
		}

		u, err := s.users.GetByID(ctx, flag.UserID)
		if err != nil {
			slog.Error("checkin_reminder: load user failed",
				"user_id", flag.UserID.String(),
				"err", err,
			)
			out.EmailFailed++
			out.PushFailed++
			continue
		}
		if u == nil || !u.IsActive {
			out.EmailSkipped++
			out.PushSkipped++
			continue
		}

		checkedIn, err := s.checks.HasCheckedInToday(ctx, u.ID)
		if err != nil {
			slog.Error("checkin_reminder: HasCheckedInToday failed",
				"user_id", u.ID.String(),
				"err", err,
			)
			out.EmailFailed++
			out.PushFailed++
			continue
		}
		if checkedIn || flag.CheckedInToday {
			slog.Info("checkin_reminder: skip — checked_in_today",
				"user_id", u.ID.String(),
				"kind", string(kind),
			)
			out.EmailSkipped++
			out.PushSkipped++
			continue
		}

		if emailReady {
			s.deliverEmail(ctx, u, kind, &out)
		} else {
			out.EmailSkipped++
		}

		if pushReady {
			s.deliverPush(ctx, u.ID, kind, &out)
		} else {
			out.PushSkipped++
		}
	}

	slog.Info("checkin_reminder: deliver end",
		"candidates", out.Candidates,
		"email_sent", out.EmailSent,
		"email_skipped", out.EmailSkipped,
		"email_failed", out.EmailFailed,
		"push_sent", out.PushSent,
		"push_skipped", out.PushSkipped,
		"push_failed", out.PushFailed,
	)
	return out, nil
}

func (s *Service) deliverEmail(
	ctx context.Context,
	u *domain.User,
	kind Kind,
	out *DeliveryResult,
) {
	if u.EmailUnsubscribedAt != nil {
		slog.Info("checkin_reminder: email skip — unsubscribed",
			"user_id", u.ID.String(),
			"kind", string(kind),
		)
		out.EmailSkipped++
		return
	}
	addr := strings.TrimSpace(u.Email)
	if addr == "" {
		out.EmailSkipped++
		return
	}
	if _, err := mail.ParseAddress(addr); err != nil {
		slog.Info("checkin_reminder: email skip — invalid address",
			"user_id", u.ID.String(),
		)
		out.EmailSkipped++
		return
	}

	claimed, err := s.emailReceipts.TryClaim(ctx, u.ID, string(kind))
	if err != nil {
		slog.Error("checkin_reminder: email claim failed",
			"user_id", u.ID.String(),
			"kind", string(kind),
			"err", err,
		)
		out.EmailFailed++
		return
	}
	if !claimed {
		slog.Info("checkin_reminder: email skip — already sent",
			"user_id", u.ID.String(),
			"kind", string(kind),
		)
		out.EmailSkipped++
		return
	}

	unsub := ""
	if s.unsub != nil {
		unsub = s.unsub.URL(u.ID)
	}
	tpl := email.BuildReminderTemplate(email.Kind(kind), s.checkInURL, unsub, u.DisplayName)
	err = s.mailer.Send(ctx, email.Message{
		To:          addr,
		Subject:     tpl.Subject,
		Text:        tpl.Text,
		HTML:        tpl.HTML,
		Unsubscribe: unsub,
	})
	if err != nil {
		if relErr := s.emailReceipts.Release(ctx, u.ID, string(kind)); relErr != nil {
			slog.Error("checkin_reminder: email receipt release failed",
				"user_id", u.ID.String(),
				"err", relErr,
			)
		}
		if errors.Is(err, email.ErrNotConfigured) {
			out.EmailSkipped++
			return
		}
		slog.Error("checkin_reminder: email send failed",
			"user_id", u.ID.String(),
			"kind", string(kind),
			"err", err,
		)
		out.EmailFailed++
		return
	}
	out.EmailSent++
}

func (s *Service) deliverPush(
	ctx context.Context,
	userID uuid.UUID,
	kind Kind,
	out *DeliveryResult,
) {
	err := s.push.SendD0D1ReminderToUser(ctx, userID, string(kind))
	switch {
	case err == nil:
		out.PushSent++
	case errors.Is(err, pushuc.ErrAlreadyNotifiedToday),
		errors.Is(err, pushuc.ErrAlreadyCheckedIn),
		errors.Is(err, pushuc.ErrNotFound),
		errors.Is(err, pushuc.ErrSenderUnavailable):
		out.PushSkipped++
	default:
		out.PushFailed++
	}
}
