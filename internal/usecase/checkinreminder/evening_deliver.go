package checkinreminder

import (
	"context"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/reminder"
	"github.com/dadiary/backend/internal/streaktime"
)

// DeliverEveningEmails sends the 19:30 ICT D1 and Day-3 reminder emails.
//
// The candidate query calls reminder.ExcludeMuted for this job, so
// reminder_enabled = false is left out. NULL and true still receive the
// email. Send time stays the shared 19:30–21:30 window.
//
// TODO: do not send at the user's ReminderTime yet. Future rules: max 1
// reminder per user per day, and skip if the user already checked in that day.
//
// D1: calendar day after the user's first check-in, and they have not checked
// in that Vietnam day. D3: three Vietnam days after the first check-in, only
// when they did not check in on day 2 (first check-in + 2 days) and have not
// checked in today. Each kind is once per user via email_send_receipts.
//
// This does not send D0 and does not send push. The hourly job still owns D0
// email and the signup-anchored D0/D1 push.
func (s *Service) DeliverEveningEmails(ctx context.Context) (DeliveryResult, error) {
	var out DeliveryResult
	if err := s.ready(); err != nil {
		return out, err
	}
	emailReady := s.mailer != nil && s.mailer.Configured() && s.emailReceipts != nil
	if !emailReady {
		slog.Info("checkin_reminder: evening email no-op — ESP not configured (set RESEND_API_KEY and EMAIL_FROM)")
		return out, nil
	}

	now := s.clock()
	today := streaktime.DateOf(now)
	cohorts, err := s.checks.ListUsersByFirstCheckDates(ctx, []time.Time{
		today.AddDate(0, 0, -1),
		today.AddDate(0, 0, -3),
	}, 5000, reminder.JobEveningEmail)
	if err != nil {
		return out, err
	}

	var scratch deliveryScratch
	var d1Sent, d3Sent int
	for _, cohort := range cohorts {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		u, err := s.users.GetByID(ctx, cohort.UserID)
		if err != nil {
			slog.Error("checkin_reminder: evening load user failed",
				"user_id", cohort.UserID.String(),
				"err", err,
			)
			out.EmailFailed++
			continue
		}
		if u == nil {
			out.EmailSkipped++
			continue
		}
		checkedToday, err := s.checks.HasCheckedInOn(ctx, u.ID, today)
		if err != nil {
			slog.Error("checkin_reminder: evening HasCheckedInOn today failed",
				"user_id", u.ID.String(),
				"err", err,
			)
			out.EmailFailed++
			continue
		}
		first := streaktime.DateOf(cohort.FirstCheck)
		checkedDay2 := false
		if today.Equal(first.AddDate(0, 0, 3)) {
			checkedDay2, err = s.checks.HasCheckedInOn(ctx, u.ID, first.AddDate(0, 0, 2))
			if err != nil {
				slog.Error("checkin_reminder: evening day-2 lookup failed",
					"user_id", u.ID.String(),
					"err", err,
				)
				out.EmailFailed++
				continue
			}
		}
		state := SelectEveningEmail(EveningInput{
			FirstCheckDate:  first,
			Now:             now,
			CheckedInToday:  checkedToday,
			CheckedInOnDay2: checkedDay2,
			AccountActive:   u.IsActive,
		})
		if !state.Due {
			out.EmailSkipped++
			continue
		}
		before := out.EmailSent
		s.deliverEmail(ctx, u, state.Kind, &out, &scratch)
		if out.EmailSent > before {
			switch state.Kind {
			case KindD1:
				d1Sent++
			case KindD3:
				d3Sent++
			}
		}
	}
	s.settleReminderRejections(ctx, &scratch, &out)

	slog.Info("checkin_reminder: evening email end",
		"candidates", len(cohorts),
		"d1_sent", d1Sent,
		"d3_sent", d3Sent,
		"email_sent", out.EmailSent,
		"email_skipped", out.EmailSkipped,
		"email_failed", out.EmailFailed,
	)
	out.Candidates = len(cohorts)
	return out, nil
}
