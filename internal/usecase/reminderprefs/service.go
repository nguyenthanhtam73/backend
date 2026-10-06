// Package reminderprefs stores the push permission card skip and its one re-show.
package reminderprefs

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

const (
	// ActionSkip records that the user dismissed the push permission card.
	ActionSkip = "skip_push_opt_in"
	// ActionConsumeReshow marks the single allowed re-show as used.
	ActionConsumeReshow = "consume_push_opt_in_reshow"
)

// ErrUnavailable means the store is not wired.
var ErrUnavailable = errors.New("reminder prefs unavailable")

// ErrInvalidAction means the client sent an unknown push_opt_in_action.
var ErrInvalidAction = errors.New("invalid push opt-in action")

// View is GET /api/v1/me/reminder.
//
// PushOptInReshowEligible is true once, starting the Vietnam civil day that is
// 3 days after the skip, until the client consumes it. The client must render
// the card only immediately after a successful check-in — never on a bare app
// open. PushOptInShowAfterCheckInOnly is always true so that rule is explicit.
type View struct {
	PushOptInSkippedAt            *time.Time `json:"push_opt_in_skipped_at,omitempty"`
	PushOptInReshowEligible       bool       `json:"push_opt_in_reshow_eligible"`
	PushOptInShowAfterCheckInOnly bool       `json:"push_opt_in_show_after_check_in_only"`
}

// Service reads and writes reminder preferences for one user.
type Service struct {
	users *repository.GormUserRepository
	now   func() time.Time
}

// NewService constructs the service.
func NewService(users *repository.GormUserRepository) *Service {
	return &Service{users: users, now: time.Now}
}

// Get returns the current push opt-in re-show flag.
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (View, error) {
	var zero View
	if s == nil || s.users == nil {
		return zero, ErrUnavailable
	}
	if userID == uuid.Nil {
		return zero, errors.New("user id required")
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return zero, err
	}
	if u == nil {
		return zero, domain.NotFound("user_not_found", "user not found")
	}
	return viewFor(u, s.clock()), nil
}

// Apply records a skip or consumes the one re-show. Both calls are idempotent.
func (s *Service) Apply(ctx context.Context, userID uuid.UUID, action string) (View, error) {
	var zero View
	if s == nil || s.users == nil {
		return zero, ErrUnavailable
	}
	if userID == uuid.Nil {
		return zero, errors.New("user id required")
	}
	now := s.clock()
	switch strings.TrimSpace(action) {
	case ActionSkip:
		if err := s.users.SetPushOptInSkippedAt(ctx, userID, now); err != nil {
			return zero, err
		}
	case ActionConsumeReshow:
		current, err := s.users.GetByID(ctx, userID)
		if err != nil {
			return zero, err
		}
		if current == nil {
			return zero, domain.NotFound("user_not_found", "user not found")
		}
		// A consume before any skip does not burn the one re-show.
		if current.PushOptInSkippedAt != nil {
			if err := s.users.SetPushOptInReshowUsedAt(ctx, userID, now); err != nil {
				return zero, err
			}
		}
	default:
		return zero, ErrInvalidAction
	}
	return s.Get(ctx, userID)
}

func (s *Service) clock() time.Time {
	if s == nil || s.now == nil {
		return time.Now()
	}
	return s.now()
}

func viewFor(u *domain.User, now time.Time) View {
	v := View{PushOptInShowAfterCheckInOnly: true}
	if u == nil {
		return v
	}
	v.PushOptInSkippedAt = u.PushOptInSkippedAt
	v.PushOptInReshowEligible = ReshowEligible(u.PushOptInSkippedAt, u.PushOptInReshowUsedAt, now)
	return v
}

// ReshowEligible is true on the Vietnam civil day 3 days after the skip, once,
// until usedAt is set. The caller still must show the card only after check-in.
func ReshowEligible(skippedAt, usedAt *time.Time, now time.Time) bool {
	if skippedAt == nil || skippedAt.IsZero() || usedAt != nil {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	skipDay := streaktime.DateOf(*skippedAt)
	today := streaktime.DateOf(now)
	days := int(today.Sub(skipDay).Hours() / 24)
	return days >= 3
}
