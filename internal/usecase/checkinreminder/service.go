package checkinreminder

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

// Service computes D0/D1 reminder state and persists flags the app can poll.
type Service struct {
	users  *repository.GormUserRepository
	checks *repository.GormSkinCheckRepository
	flags  *repository.CheckInReminderRepository
	now    func() time.Time
	// vapidConfigured is true when the evening daily_reminder job can send.
	vapidConfigured bool
}

// NewService wires reminder deps. now may be nil (uses time.Now).
func NewService(
	users *repository.GormUserRepository,
	checks *repository.GormSkinCheckRepository,
	flags *repository.CheckInReminderRepository,
	vapidConfigured bool,
) *Service {
	return &Service{
		users:           users,
		checks:          checks,
		flags:           flags,
		now:             time.Now,
		vapidConfigured: vapidConfigured,
	}
}

func (s *Service) ready() error {
	if s == nil || s.users == nil || s.checks == nil || s.flags == nil {
		return fmt.Errorf("check-in reminder service unavailable")
	}
	return nil
}

func (s *Service) clock() time.Time {
	if s == nil || s.now == nil {
		return time.Now()
	}
	return s.now()
}

// GetForUser recomputes the current user's D0/D1 state and upserts the flag.
func (s *Service) GetForUser(ctx context.Context, userID uuid.UUID) (dto.CheckInReminderResponse, error) {
	var zero dto.CheckInReminderResponse
	if err := s.ready(); err != nil {
		return zero, domain.Unavailable("service_unavailable", err.Error())
	}
	if userID == uuid.Nil {
		return zero, domain.BadRequest("invalid_input", "missing user id")
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return zero, err
	}
	if u == nil {
		return zero, domain.NotFound("user_not_found", "user not found")
	}
	state, err := s.compute(ctx, u)
	if err != nil {
		return zero, err
	}
	if err := s.persist(ctx, u.ID, state); err != nil {
		slog.Warn("checkin_reminder: persist failed",
			"user_id", userID.String(),
			"err", err,
		)
	}
	return s.toDTO(state), nil
}

// RefreshWindow recomputes flags for every D0/D1 candidate (signup yesterday
// or today in Vietnam) plus anyone still marked due. Idempotent.
func (s *Service) RefreshWindow(ctx context.Context) (dto.CheckInReminderRefreshResponse, error) {
	var out dto.CheckInReminderRefreshResponse
	if err := s.ready(); err != nil {
		return out, err
	}
	now := s.clock()
	from, to := SignupWindow(now)

	recent, err := s.users.ListCreatedBetween(ctx, from, to, 5000)
	if err != nil {
		return out, err
	}
	dueIDs, err := s.flags.ListDueUserIDs(ctx, 2000)
	if err != nil {
		return out, err
	}

	seen := make(map[uuid.UUID]*domain.User, len(recent)+len(dueIDs))
	for i := range recent {
		seen[recent[i].ID] = &recent[i]
	}
	for _, id := range dueIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		u, err := s.users.GetByID(ctx, id)
		if err != nil {
			return out, err
		}
		if u != nil {
			seen[id] = u
		}
	}

	for _, u := range seen {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		state, err := s.compute(ctx, u)
		if err != nil {
			slog.Error("checkin_reminder: compute failed",
				"user_id", u.ID.String(),
				"err", err,
			)
			continue
		}
		if err := s.persist(ctx, u.ID, state); err != nil {
			slog.Error("checkin_reminder: upsert failed",
				"user_id", u.ID.String(),
				"err", err,
			)
			continue
		}
		out.Scanned++
		out.Upserted++
		if !state.Due {
			if state.Kind == KindNone {
				out.Cleared++
			}
			continue
		}
		switch state.Kind {
		case KindD0:
			out.DueD0++
		case KindD1:
			out.DueD1++
		}
	}

	slog.Info("checkin_reminder: refresh complete",
		"scanned", out.Scanned,
		"due_d0", out.DueD0,
		"due_d1", out.DueD1,
		"cleared", out.Cleared,
		"from", from.Format(time.RFC3339),
		"to", to.Format(time.RFC3339),
	)
	return out, nil
}

func (s *Service) compute(ctx context.Context, u *domain.User) (State, error) {
	checkedIn, err := s.checks.HasCheckedInToday(ctx, u.ID)
	if err != nil {
		return State{}, err
	}
	return Select(Input{
		SignupAt:       u.CreatedAt,
		Now:            s.clock(),
		CheckedInToday: checkedIn,
		AccountActive:  u.IsActive,
	}), nil
}

func (s *Service) persist(ctx context.Context, userID uuid.UUID, state State) error {
	signupDay := streaktime.Today()
	if state.SignupDate != "" {
		if t, err := time.Parse("2006-01-02", state.SignupDate); err == nil {
			signupDay = t
		}
	}
	row := &domain.CheckInReminderFlag{
		UserID:          userID,
		Kind:            string(state.Kind),
		Due:             state.Due,
		SignupDate:      signupDay,
		CheckedInToday:  state.CheckedInToday,
		DaysSinceSignup: state.DaysSinceSignup,
		ComputedOn:      streaktime.DateOf(s.clock()),
		ComputedAt:      time.Now().UTC(),
	}
	return s.flags.Upsert(ctx, row)
}

func (s *Service) toDTO(state State) dto.CheckInReminderResponse {
	return dto.CheckInReminderResponse{
		Kind:            string(state.Kind),
		Due:             state.Due,
		SignupDate:      state.SignupDate,
		DaysSinceSignup: state.DaysSinceSignup,
		CheckedInToday:  state.CheckedInToday,
		Channels:        s.channels(),
	}
}

func (s *Service) channels() dto.CheckInReminderChannels {
	ch := dto.CheckInReminderChannels{
		InApp:            true,
		Email:            false,
		EmailReason:      "no_outbound_email",
		PushEvening:      s != nil && s.vapidConfigured,
		PushD0D1Specific: false,
		PushNote:         "evening_daily_reminder_exists_not_d0_d1_specific",
	}
	return ch
}
