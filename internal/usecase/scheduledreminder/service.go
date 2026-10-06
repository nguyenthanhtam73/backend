package scheduledreminder

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/reminder"
	"github.com/dadiary/backend/internal/repository"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/dadiary/backend/internal/streaktime"
	checkinreminderuc "github.com/dadiary/backend/internal/usecase/checkinreminder"
	pushuc "github.com/dadiary/backend/internal/usecase/push"
	"github.com/google/uuid"
)

// Result is one tick of the per-user capture reminder.
type Result struct {
	Candidates int
	Sent       int
	Skipped    int
	Failed     int
	PushSent   int
	EmailSent  int
}

type streakSource interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Streak, error)
}

// Service sends one capture moment at each saved local HH:MM.
type Service struct {
	users   *repository.GormUserRepository
	checks  *repository.GormSkinCheckRepository
	claims  *repository.CaptureReminderClaimRepository
	push    *pushuc.Service
	streaks streakSource
	emails  *checkinreminderuc.Service
	now     func() time.Time
}

// NewService wires the scheduled capture sender. streaks and emails may be nil.
func NewService(
	users *repository.GormUserRepository,
	checks *repository.GormSkinCheckRepository,
	claims *repository.CaptureReminderClaimRepository,
	push *pushuc.Service,
	streaks streakSource,
	emails *checkinreminderuc.Service,
) *Service {
	return &Service{
		users:   users,
		checks:  checks,
		claims:  claims,
		push:    push,
		streaks: streaks,
		emails:  emails,
		now:     time.Now,
	}
}

// SetNow replaces the clock. Tests use it.
func (s *Service) SetNow(now func() time.Time) {
	if s == nil || now == nil {
		return
	}
	s.now = now
}

func (s *Service) clock() time.Time {
	if s == nil || s.now == nil {
		return time.Now()
	}
	return s.now()
}

// Deliver sends the capture moment for every saved schedule whose local window
// is open. Users who already checked in on that local civil day are skipped.
// The claim row stops a second tick or a second replica from sending again.
func (s *Service) Deliver(ctx context.Context) (Result, error) {
	var out Result
	if s == nil || s.users == nil || s.checks == nil || s.claims == nil {
		return out, errors.New("scheduled capture unavailable")
	}
	users, err := s.users.ListSavedReminderUsers(ctx, 5000, reminder.JobScheduledCapture)
	if err != nil {
		return out, err
	}
	out.Candidates = len(users)
	now := s.clock()
	for i := range users {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		sent, failed, pushSent, emailSent := s.deliverOne(ctx, &users[i], now)
		switch {
		case sent:
			out.Sent++
		case failed:
			out.Failed++
		default:
			out.Skipped++
		}
		if pushSent {
			out.PushSent++
		}
		if emailSent {
			out.EmailSent++
		}
	}
	if out.Sent > 0 || out.Failed > 0 {
		slog.Info("scheduled_capture: tick",
			"candidates", out.Candidates,
			"sent", out.Sent,
			"skipped", out.Skipped,
			"failed", out.Failed,
			"push_sent", out.PushSent,
			"email_sent", out.EmailSent,
		)
	}
	return out, nil
}

// deliverOne returns sent when at least one channel landed, failed when every
// attempt failed, and both false when the user was not due.
func (s *Service) deliverOne(ctx context.Context, u *domain.User, now time.Time) (sent, failed, pushSent, emailSent bool) {
	if u == nil || !reminder.SavedSchedule(u.ReminderEnabled, u.ReminderTime) {
		return false, false, false, false
	}
	loc := reminder.Zone(u.ReminderTimezone)
	hhmm := ""
	if u.ReminderTime != nil {
		hhmm = *u.ReminderTime
	}
	localDate, open := reminder.OpenWindow(now, loc, hhmm, reminder.CaptureGrace)
	if !open {
		return false, false, false, false
	}
	start, end := reminder.LocalDay(now, loc)
	checked, err := s.checks.HasCheckedInBetween(ctx, u.ID, start, end)
	if err != nil {
		slog.Error("scheduled_capture: check-in lookup failed",
			"user_id", u.ID.String(),
			"err", err,
		)
		return false, true, false, false
	}
	if checked {
		slog.Debug("scheduled_capture: skip — checked in on local day",
			"user_id", u.ID.String(),
			"local_date", localDate,
		)
		return false, false, false, false
	}

	pushType, pushErr := s.pushType(ctx, u, now)
	if pushErr != nil {
		slog.Error("scheduled_capture: push type failed",
			"user_id", u.ID.String(),
			"err", pushErr,
		)
		return false, true, false, false
	}
	emailKind, emailErr := s.emailKind(ctx, u, now)
	if emailErr != nil {
		slog.Error("scheduled_capture: email kind failed",
			"user_id", u.ID.String(),
			"err", emailErr,
		)
		return false, true, false, false
	}
	tryPush := s.push != nil && pushType != ""
	tryEmail := s.emails != nil && emailKind != checkinreminderuc.KindNone

	claimed, err := s.claims.TryClaim(ctx, u.ID, localDate, now)
	if err != nil {
		slog.Error("scheduled_capture: claim failed",
			"user_id", u.ID.String(),
			"local_date", localDate,
			"err", err,
		)
		return false, true, false, false
	}
	if !claimed {
		slog.Debug("scheduled_capture: skip — already claimed",
			"user_id", u.ID.String(),
			"local_date", localDate,
		)
		return false, false, false, false
	}

	// Push sender is not wired and no D0/D1/Day-3 email is due.
	// The claim stays for this local civil day so later ticks do not retry.
	if !tryPush && !tryEmail {
		slog.Debug("scheduled_capture: nothing to send — claim kept",
			"user_id", u.ID.String(),
			"local_date", localDate,
		)
		return false, false, false, false
	}

	pushSent, pushFailed := s.sendPush(ctx, u.ID, pushType, localDate, tryPush)
	emailSent, emailFailed := s.sendEmail(ctx, u, emailKind, tryEmail)
	if pushSent || emailSent {
		slog.Info("scheduled_capture: sent",
			"user_id", u.ID.String(),
			"local_date", localDate,
			"zone", loc.String(),
			"push", pushLabel(pushSent, pushType),
			"email", emailLabel(emailSent, emailKind),
		)
		return true, false, pushSent, emailSent
	}
	// A skipped push (no subscription, sender not configured) is not a failed
	// attempt. Release only when a send was attempted and the provider failed,
	// so a later tick inside the grace window can retry that attempt.
	if pushFailed || emailFailed {
		s.release(ctx, u.ID, localDate)
		return false, true, false, false
	}
	slog.Debug("scheduled_capture: nothing to send — claim kept",
		"user_id", u.ID.String(),
		"local_date", localDate,
	)
	return false, false, false, false
}

func (s *Service) sendPush(
	ctx context.Context,
	userID uuid.UUID,
	nType pushsvc.NotificationType,
	localDate string,
	try bool,
) (sent bool, failed bool) {
	if !try {
		return false, false
	}
	err := s.push.SendScheduledCapture(ctx, userID, nType, localDate)
	switch {
	case err == nil:
		return true, false
	case errors.Is(err, pushuc.ErrNotFound), errors.Is(err, pushuc.ErrSenderUnavailable):
		return false, false
	default:
		slog.Error("scheduled_capture: push failed",
			"user_id", userID.String(),
			"type", string(nType),
			"err", err,
		)
		return false, true
	}
}

func (s *Service) sendEmail(
	ctx context.Context,
	u *domain.User,
	kind checkinreminderuc.Kind,
	try bool,
) (sent bool, failed bool) {
	if !try || u == nil {
		return false, false
	}
	sent, failed = s.emails.SendReminderEmail(ctx, u, kind)
	if failed {
		slog.Error("scheduled_capture: email failed",
			"user_id", u.ID.String(),
			"kind", string(kind),
		)
	}
	return sent, failed
}

func (s *Service) release(ctx context.Context, userID uuid.UUID, localDate string) {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.claims.Release(releaseCtx, userID, localDate); err != nil {
		slog.Error("scheduled_capture: release claim failed",
			"user_id", userID.String(),
			"local_date", localDate,
			"err", err,
		)
	}
}

// pushType picks the single push for this moment. Streak at risk wins, matching
// the 20:00 mutual exclusion. Otherwise a due signup D0/D1 push, else the
// daily check-in nudge. The hourly d0/d1 job does not also send.
func (s *Service) pushType(ctx context.Context, u *domain.User, now time.Time) (pushsvc.NotificationType, error) {
	risk, err := s.atRisk(ctx, u.ID)
	if err != nil {
		return "", err
	}
	if risk {
		return pushsvc.NotificationTypeStreakAtRisk, nil
	}
	state, err := s.signupState(ctx, u, now)
	if err != nil {
		return "", err
	}
	if state.Due && state.Kind == checkinreminderuc.KindD0 {
		return pushsvc.NotificationTypeD0Reminder, nil
	}
	if state.Due && state.Kind == checkinreminderuc.KindD1 {
		return pushsvc.NotificationTypeD1Reminder, nil
	}
	return pushsvc.NotificationTypeDailyReminder, nil
}

func (s *Service) atRisk(ctx context.Context, userID uuid.UUID) (bool, error) {
	if s == nil || s.streaks == nil {
		return false, nil
	}
	row, err := s.streaks.GetByUserID(ctx, userID)
	if err != nil || row == nil {
		return false, err
	}
	return dto.EvaluateStreakView(row, streaktime.Today()).IsAtRisk, nil
}

// emailKind is the email half of the same moment: D0 on the signup day, else
// the evening D1/Day-3 email when that cohort is due. Neither is a second moment.
func (s *Service) emailKind(ctx context.Context, u *domain.User, now time.Time) (checkinreminderuc.Kind, error) {
	state, err := s.signupState(ctx, u, now)
	if err != nil {
		return checkinreminderuc.KindNone, err
	}
	if state.Due && state.Kind == checkinreminderuc.KindD0 {
		return checkinreminderuc.KindD0, nil
	}
	first, err := s.checks.FirstCheckDate(ctx, u.ID)
	if err != nil {
		return checkinreminderuc.KindNone, err
	}
	if first == nil {
		return checkinreminderuc.KindNone, nil
	}
	today := streaktime.DateOf(now)
	checkedToday, err := s.checks.HasCheckedInOn(ctx, u.ID, today)
	if err != nil {
		return checkinreminderuc.KindNone, err
	}
	checkedDay2 := false
	anchor := streaktime.DateOf(*first)
	if today.Equal(anchor.AddDate(0, 0, 3)) {
		checkedDay2, err = s.checks.HasCheckedInOn(ctx, u.ID, anchor.AddDate(0, 0, 2))
		if err != nil {
			return checkinreminderuc.KindNone, err
		}
	}
	ev := checkinreminderuc.SelectEveningEmail(checkinreminderuc.EveningInput{
		FirstCheckDate:  anchor,
		Now:             now,
		CheckedInToday:  checkedToday,
		CheckedInOnDay2: checkedDay2,
		AccountActive:   u.IsActive,
	})
	if ev.Due {
		return ev.Kind, nil
	}
	return checkinreminderuc.KindNone, nil
}

func (s *Service) signupState(ctx context.Context, u *domain.User, now time.Time) (checkinreminderuc.State, error) {
	checked, err := s.checks.HasCheckedInToday(ctx, u.ID)
	if err != nil {
		return checkinreminderuc.State{}, err
	}
	return checkinreminderuc.Select(checkinreminderuc.Input{
		SignupAt:       u.CreatedAt,
		Now:            now,
		CheckedInToday: checked,
		AccountActive:  u.IsActive,
	}), nil
}

func pushLabel(sent bool, nType pushsvc.NotificationType) string {
	if !sent {
		return ""
	}
	return string(nType)
}

func emailLabel(sent bool, kind checkinreminderuc.Kind) string {
	if !sent || kind == checkinreminderuc.KindNone {
		return ""
	}
	return string(kind)
}
