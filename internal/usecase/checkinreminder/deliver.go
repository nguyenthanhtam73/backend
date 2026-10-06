package checkinreminder

import (
	"context"
	"errors"
	"log/slog"
	"net/mail"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/reminder"
	"github.com/dadiary/backend/internal/service/email"
	pushuc "github.com/dadiary/backend/internal/usecase/push"
	"github.com/google/uuid"
)

// deliveryScratch collects non-recipient rejections for one DeliverDue run so
// a sender-wide failure is not written onto every user.
type deliveryScratch struct {
	sendAttempts int
	rejections   []pendingRejection
}

type pendingRejection struct {
	user  *domain.User
	hash  string
	state reminderEmailState
	kind  Kind
	err   error
}

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

	rows, err := s.flags.ListDue(ctx, 2000, reminder.JobD0Email, reminder.JobD0D1Push)
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

	var scratch deliveryScratch
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

		// D0 email stays on this hourly pass (same-day signup, no check-in).
		// D1 email moved to the 19:30 ICT job and is anchored on first check-in,
		// not this signup-day flag.
		// reminder_enabled = false is already absent: ListDue calls
		// reminder.ExcludeMuted for the hourly D0 email and the D0/D1 push.
		// NULL and true stay in this list. Transactional mail does not come
		// through here.
		if kind == KindD0 && emailReady {
			s.deliverEmail(ctx, u, kind, &out, &scratch)
		} else {
			out.EmailSkipped++
		}

		if pushReady {
			s.deliverPush(ctx, u.ID, kind, &out)
		} else {
			out.PushSkipped++
		}
	}

	s.settleReminderRejections(ctx, &scratch, &out)

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
	scratch *deliveryScratch,
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

	hash := email.AddressHash(addr)
	state := reminderStateFromUser(u)
	if reminderEmailBlocked(state, hash) {
		slog.Info("checkin_reminder: email skip — undeliverable address",
			"user_id", u.ID.String(),
			"kind", string(kind),
		)
		out.EmailSkipped++
		return
	}
	if state.AddressHash != "" && state.AddressHash != hash {
		if err := s.users.SaveReminderEmailState(ctx, u.ID, 0, "", nil); err != nil {
			slog.Error("checkin_reminder: email suppression clear failed",
				"user_id", u.ID.String(),
				"err", err,
			)
		} else {
			state = reminderEmailState{}
		}
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
	if scratch != nil {
		scratch.sendAttempts++
	}
	providerID, err := s.mailer.Send(ctx, email.Message{
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
		class := email.Classify(err)
		if class == email.FailureTransient {
			slog.Error("checkin_reminder: email send failed",
				"user_id", u.ID.String(),
				"kind", string(kind),
				"err", err,
			)
			out.EmailFailed++
			return
		}
		if class != email.FailurePermanent && scratch != nil {
			scratch.rejections = append(scratch.rejections, pendingRejection{
				user:  u,
				hash:  hash,
				state: state,
				kind:  kind,
				err:   err,
			})
			return
		}
		s.recordReminderRejection(ctx, pendingRejection{
			user:  u,
			hash:  hash,
			state: state,
			kind:  kind,
			err:   err,
		}, class == email.FailurePermanent, out)
		return
	}
	if providerID != "" {
		if setErr := s.emailReceipts.SetResendEmailID(ctx, u.ID, string(kind), providerID); setErr != nil {
			slog.Error("checkin_reminder: store resend email id failed",
				"user_id", u.ID.String(),
				"kind", string(kind),
				"err", setErr,
			)
		}
	}
	if state.FailCount > 0 || state.AddressHash != "" || state.SuppressedAt != nil {
		if clrErr := s.users.SaveReminderEmailState(ctx, u.ID, 0, "", nil); clrErr != nil {
			slog.Error("checkin_reminder: email suppression clear failed",
				"user_id", u.ID.String(),
				"err", clrErr,
			)
		}
	}
	out.EmailSent++
}

// settleReminderRejections applies 3-strike counters after the run, unless the
// same non-recipient error hit many users — that is a sender/config problem.
func (s *Service) settleReminderRejections(
	ctx context.Context,
	scratch *deliveryScratch,
	out *DeliveryResult,
) {
	if s == nil || scratch == nil || len(scratch.rejections) == 0 {
		return
	}
	grouped := map[string][]pendingRejection{}
	var order []string
	for _, hit := range scratch.rejections {
		key := email.FailureKey(hit.err)
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], hit)
	}
	for _, key := range order {
		hits := grouped[key]
		users := map[uuid.UUID]struct{}{}
		for _, hit := range hits {
			if hit.user != nil {
				users[hit.user.ID] = struct{}{}
			}
		}
		if reminderSenderWide(len(users), scratch.sendAttempts) {
			status := 0
			name := ""
			if len(hits) > 0 {
				status = email.StatusOf(hits[0].err)
				name = email.ResendName(hits[0].err)
			}
			slog.Error("checkin_reminder: sender error, not counting recipient failures",
				"http_status", status,
				"resend_error", name,
				"users", len(users),
				"send_attempts", scratch.sendAttempts,
			)
			if out != nil {
				out.EmailFailed += len(hits)
			}
			continue
		}
		for _, hit := range hits {
			s.recordReminderRejection(ctx, hit, false, out)
		}
	}
}

func (s *Service) recordReminderRejection(
	ctx context.Context,
	hit pendingRejection,
	permanent bool,
	out *DeliveryResult,
) {
	if hit.user == nil {
		if out != nil {
			out.EmailFailed++
		}
		return
	}
	userID := hit.user.ID.String()
	next, newly := applyReminderEmailRejection(hit.state, hit.hash, permanent, s.clock())
	if saveErr := s.users.SaveReminderEmailState(
		ctx, hit.user.ID, next.FailCount, next.AddressHash, next.SuppressedAt,
	); saveErr != nil {
		slog.Error("checkin_reminder: email suppression save failed",
			"user_id", userID,
			"err", saveErr,
		)
		slog.Error("checkin_reminder: email send failed",
			"user_id", userID,
			"kind", string(hit.kind),
			"err", hit.err,
		)
		if out != nil {
			out.EmailFailed++
		}
		return
	}
	if newly {
		slog.Warn("checkin_reminder: email address marked undeliverable",
			"user_id", userID,
			"kind", string(hit.kind),
			"http_status", email.StatusOf(hit.err),
			"fail_count", next.FailCount,
		)
		if out != nil {
			out.EmailSkipped++
		}
		return
	}
	slog.Error("checkin_reminder: email send failed",
		"user_id", userID,
		"kind", string(hit.kind),
		"err", hit.err,
	)
	if out != nil {
		out.EmailFailed++
	}
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
