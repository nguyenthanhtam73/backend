// Package usage enforces free-plan quotas and premium gates for beta.
package usage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

const (
	FreeRoutineSuggestPerMonth    = 3
	FreeRoutineManualEditPerMonth = 5
)

var (
	ErrUnavailable      = errors.New("usage service unavailable")
	ErrPremiumRequired  = errors.New("premium required for this action")
	ErrQuotaExceeded    = errors.New("monthly quota exceeded")
)

// Service checks plan tier and records monthly usage events.
type Service struct {
	users  repository.UserRepository
	events repository.UsageEventRepository
}

// NewService wires dependencies. Either repo may be nil (guards fail closed on writes).
func NewService(users repository.UserRepository, events repository.UsageEventRepository) *Service {
	return &Service{users: users, events: events}
}

func (s *Service) planTier(ctx context.Context, userID uuid.UUID) (domain.PlanTier, error) {
	if s == nil || s.users == nil {
		return domain.PlanFree, fmt.Errorf("%w", ErrUnavailable)
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil || u == nil {
		return domain.PlanFree, err
	}
	if u.PlanTier == domain.PlanPremium {
		return domain.PlanPremium, nil
	}
	return domain.PlanFree, nil
}

func (s *Service) IsPremium(ctx context.Context, userID uuid.UUID) (bool, error) {
	tier, err := s.planTier(ctx, userID)
	if err != nil {
		return false, err
	}
	return tier == domain.PlanPremium, nil
}

func monthStartUTC(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func (s *Service) countFeature(ctx context.Context, userID uuid.UUID, feature domain.UsageFeature) (int, error) {
	if s == nil || s.events == nil {
		return 0, fmt.Errorf("%w", ErrUnavailable)
	}
	n, err := s.events.CountSince(ctx, userID, feature, monthStartUTC(time.Now()))
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func buildCounter(used, limit int, unlimited bool) dto.UsageCounter {
	if unlimited {
		return dto.UsageCounter{Used: used, Limit: 0, Remaining: 0, Unlimited: true}
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	return dto.UsageCounter{Used: used, Limit: limit, Remaining: remaining}
}

// GetQuota returns the user's current plan and monthly counters (UTC month).
func (s *Service) GetQuota(ctx context.Context, userID uuid.UUID) (dto.UsageQuotaResponse, error) {
	var out dto.UsageQuotaResponse
	if userID == uuid.Nil {
		return out, fmt.Errorf("%w: user id required", ErrUnavailable)
	}
	tier, err := s.planTier(ctx, userID)
	if err != nil {
		return out, err
	}
	premium := tier == domain.PlanPremium
	now := time.Now().UTC()
	out.PlanTier = string(tier)
	if out.PlanTier == "" {
		out.PlanTier = string(domain.PlanFree)
	}
	out.IsPremium = premium
	out.Period = now.Format("2006-01")
	out.Wardrobe = dto.WardrobeUsage{CanWrite: premium}

	suggestUsed, _ := s.countFeature(ctx, userID, domain.UsageRoutineSuggest)
	editUsed, _ := s.countFeature(ctx, userID, domain.UsageRoutineManualEdit)
	if premium {
		out.RoutineSuggest = buildCounter(suggestUsed, 0, true)
		out.RoutineManualEdit = buildCounter(editUsed, 0, true)
	} else {
		out.RoutineSuggest = buildCounter(suggestUsed, FreeRoutineSuggestPerMonth, false)
		out.RoutineManualEdit = buildCounter(editUsed, FreeRoutineManualEditPerMonth, false)
	}
	return out, nil
}

// AssertWardrobeWrite blocks free users from adding/editing wardrobe items.
func (s *Service) AssertWardrobeWrite(ctx context.Context, userID uuid.UUID) error {
	premium, err := s.IsPremium(ctx, userID)
	if err != nil {
		return err
	}
	if !premium {
		return ErrPremiumRequired
	}
	return nil
}

func (s *Service) assertUnderLimit(ctx context.Context, userID uuid.UUID, feature domain.UsageFeature, limit int) error {
	premium, err := s.IsPremium(ctx, userID)
	if err != nil {
		return err
	}
	if premium {
		return nil
	}
	used, err := s.countFeature(ctx, userID, feature)
	if err != nil {
		return err
	}
	if used >= limit {
		return ErrQuotaExceeded
	}
	return nil
}

// AssertRoutineSuggest checks the monthly AI suggest quota for free users.
func (s *Service) AssertRoutineSuggest(ctx context.Context, userID uuid.UUID) error {
	return s.assertUnderLimit(ctx, userID, domain.UsageRoutineSuggest, FreeRoutineSuggestPerMonth)
}

// RecordRoutineSuggest increments the suggest counter after a successful AI call.
func (s *Service) RecordRoutineSuggest(ctx context.Context, userID uuid.UUID) error {
	return s.record(ctx, userID, domain.UsageRoutineSuggest)
}

// AssertRoutineManualEdit checks the monthly manual-edit quota for free users.
func (s *Service) AssertRoutineManualEdit(ctx context.Context, userID uuid.UUID) error {
	return s.assertUnderLimit(ctx, userID, domain.UsageRoutineManualEdit, FreeRoutineManualEditPerMonth)
}

// RecordRoutineManualEdit increments the manual-edit counter after a structural save.
func (s *Service) RecordRoutineManualEdit(ctx context.Context, userID uuid.UUID) error {
	return s.record(ctx, userID, domain.UsageRoutineManualEdit)
}

func (s *Service) record(ctx context.Context, userID uuid.UUID, feature domain.UsageFeature) error {
	premium, err := s.IsPremium(ctx, userID)
	if err != nil {
		return err
	}
	if premium {
		return nil
	}
	if s == nil || s.events == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	return s.events.Create(ctx, &domain.UsageEvent{
		UserID:  userID,
		Feature: feature,
	})
}

// IsTickOnlySave returns true when the client only persisted completion ticks.
func IsTickOnlySave(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(kind), "tick_only")
}
