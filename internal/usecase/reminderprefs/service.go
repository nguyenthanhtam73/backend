// Package reminderprefs stores the push permission card skip and its one re-show.
package reminderprefs

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	// Zone data for time.LoadLocation. cmd/api also imports this so the
	// server image, which has no zoneinfo, can accept IANA timezones.
	_ "time/tzdata"

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

// DefaultReminderTimezone is stored when a schedule write omits timezone
// or sends it blank, and this user has never stored one.
const DefaultReminderTimezone = "Asia/Ho_Chi_Minh"

// ErrUnavailable means the store is not wired.
var ErrUnavailable = errors.New("reminder prefs unavailable")

// ErrInvalidAction means the client sent an unknown push_opt_in_action,
// or a body with neither an action nor a schedule field.
var ErrInvalidAction = errors.New("invalid push opt-in action")

// ErrInvalidTime means time was not HH:MM in 00:00–23:59.
var ErrInvalidTime = errors.New("invalid reminder time")

// ErrInvalidTimezone means timezone was not an IANA location name.
var ErrInvalidTimezone = errors.New("invalid reminder timezone")

// Update is one PUT /api/v1/me/reminder.
//
// Action empty means the push-permission card is unchanged. A nil Enabled,
// Time, or Timezone leaves that column unchanged. A blank Time is treated as
// omitted. A missing or blank Timezone keeps the stored zone; it becomes
// DefaultReminderTimezone only when no timezone has ever been stored.
type Update struct {
	Action   string
	Enabled  *bool
	Time     *string
	Timezone *string
}

// Schedule is the saved reminder clock. It is omitted from GET until the
// user has saved at least one of the three fields.
type Schedule struct {
	Enabled  *bool   `json:"enabled"`
	Time     *string `json:"time"`
	Timezone *string `json:"timezone"`
}

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
	Schedule                      *Schedule  `json:"schedule,omitempty"`
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

// Apply records a push-card action, a reminder schedule, or both.
// The card actions stay idempotent. A schedule write and a card action in
// the same request are both stored. Validation runs before either write.
func (s *Service) Apply(ctx context.Context, userID uuid.UUID, in Update) (View, error) {
	var zero View
	if s == nil || s.users == nil {
		return zero, ErrUnavailable
	}
	if userID == uuid.Nil {
		return zero, errors.New("user id required")
	}
	action := strings.TrimSpace(in.Action)
	hasSchedule := in.Enabled != nil || in.Time != nil || in.Timezone != nil
	if action == "" && !hasSchedule {
		return zero, ErrInvalidAction
	}
	switch action {
	case "", ActionSkip, ActionConsumeReshow:
	default:
		return zero, ErrInvalidAction
	}

	hhmm, err := reminderTimeUpdate(in.Time)
	if err != nil {
		return zero, err
	}
	var tz *string
	if hasSchedule {
		tz, err = s.resolveReminderTimezone(ctx, userID, in.Timezone)
		if err != nil {
			return zero, err
		}
	}

	if hasSchedule {
		if err := s.users.SetReminderSchedule(ctx, userID, in.Enabled, hhmm, tz); err != nil {
			return zero, err
		}
	}
	if action == "" {
		return s.Get(ctx, userID)
	}

	now := s.clock()
	switch action {
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
	}
	return s.Get(ctx, userID)
}

// reminderTimeUpdate returns nil when time was omitted or blank, so the
// stored clock is left as-is. A non-empty value must be HH:MM.
func reminderTimeUpdate(raw *string) (*string, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := NormalizeReminderTime(*raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

// resolveReminderTimezone returns nil when the stored timezone should be kept.
// An explicit IANA name is validated and returned for writing. A missing or
// blank name keeps a stored zone, or DefaultReminderTimezone when none exists.
func (s *Service) resolveReminderTimezone(ctx context.Context, userID uuid.UUID, raw *string) (*string, error) {
	if raw != nil {
		name := strings.TrimSpace(*raw)
		if name != "" {
			resolved, err := NormalizeReminderTimezone(name)
			if err != nil {
				return nil, err
			}
			return &resolved, nil
		}
	}
	current, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, domain.NotFound("user_not_found", "user not found")
	}
	if current.ReminderTimezone != nil && strings.TrimSpace(*current.ReminderTimezone) != "" {
		return nil, nil
	}
	resolved, err := NormalizeReminderTimezone("")
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// NormalizeReminderTime accepts HH:MM from 00:00 through 23:59.
func NormalizeReminderTime(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) != 5 || raw[2] != ':' {
		return "", ErrInvalidTime
	}
	hour, errH := strconv.Atoi(raw[:2])
	minute, errM := strconv.Atoi(raw[3:])
	if errH != nil || errM != nil || raw[:2] != fmt.Sprintf("%02d", hour) || raw[3:] != fmt.Sprintf("%02d", minute) {
		return "", ErrInvalidTime
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return "", ErrInvalidTime
	}
	return raw, nil
}

// NormalizeReminderTimezone returns an IANA name. A blank value becomes
// DefaultReminderTimezone. "Local" is rejected; it is not an IANA zone.
func NormalizeReminderTimezone(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = DefaultReminderTimezone
	}
	if strings.EqualFold(name, "Local") {
		return "", ErrInvalidTimezone
	}
	if len(name) > 64 {
		return "", ErrInvalidTimezone
	}
	if _, err := time.LoadLocation(name); err != nil {
		return "", ErrInvalidTimezone
	}
	return name, nil
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
	v.Schedule = scheduleFromUser(u)
	return v
}

func scheduleFromUser(u *domain.User) *Schedule {
	if u == nil {
		return nil
	}
	enabled := u.ReminderEnabled
	hhmm := textPtr(u.ReminderTime)
	tz := textPtr(u.ReminderTimezone)
	if enabled == nil && hhmm == nil && tz == nil {
		return nil
	}
	return &Schedule{Enabled: enabled, Time: hhmm, Timezone: tz}
}

func textPtr(raw *string) *string {
	if raw == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
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
