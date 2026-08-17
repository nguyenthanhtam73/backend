// Package usage records monthly metered actions and exposes GET /me/usage.
//
// Plan × feature decisions live in usecase/premium. This package asserts via
// PremiumService and persists Free counters through user_usages (IncrementUsage).
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
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	"github.com/google/uuid"
)

// Legacy free-tier constants — kept for callers/tests; source of truth is premium catalog.
const (
	FreeRoutineSuggestPerMonth    = 3
	FreeRoutineManualEditPerMonth = 5
	// FreeWardrobeProductLimit is how many shelf items Free users may create.
	// Premium / Premium+ are unlimited. Edit/delete still require wardrobe_full.
	FreeWardrobeProductLimit = 3
)

var (
	ErrUnavailable     = errors.New("usage service unavailable")
	ErrPremiumRequired = errors.New("premium required for this action")
	ErrQuotaExceeded   = errors.New("monthly quota exceeded")
)

// ShelfCounter counts active skincare-shelf products for Free create gating.
type ShelfCounter interface {
	CountByUser(ctx context.Context, userID uuid.UUID) (int64, error)
}

// Service checks plan gates (via premium) and records monthly usage.
type Service struct {
	gates *premiumuc.Service
	shelf ShelfCounter
}

// NewService wires a private PremiumService + user_usages repo.
func NewService(users repository.UserRepository, usages repository.UserUsageRepository) *Service {
	return &Service{
		gates: premiumuc.NewService(users, usages),
	}
}

// NewWithGates injects a shared PremiumService (preferred when multiple usecases share it).
func NewWithGates(gates *premiumuc.Service) *Service {
	return &Service{gates: gates}
}

// AttachShelfCounter enables Free shelf-slot checks on create + GET /me/usage.
func (s *Service) AttachShelfCounter(counter ShelfCounter) {
	if s == nil {
		return
	}
	s.shelf = counter
}

// Gates exposes the underlying PremiumService for handlers that need richer checks.
func (s *Service) Gates() *premiumuc.Service {
	if s == nil {
		return nil
	}
	return s.gates
}

func (s *Service) planTier(ctx context.Context, userID uuid.UUID) (domain.PlanTier, error) {
	if s == nil || s.gates == nil {
		return domain.PlanFree, fmt.Errorf("%w", ErrUnavailable)
	}
	return s.gates.PlanTier(ctx, userID)
}

// IsPremium is true for Premium and Premium+ (paid plans).
func (s *Service) IsPremium(ctx context.Context, userID uuid.UUID) (bool, error) {
	if s == nil || s.gates == nil {
		return false, fmt.Errorf("%w", ErrUnavailable)
	}
	return s.gates.IsPaidPlan(ctx, userID)
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

func quotaToCounter(q premiumuc.Quota) dto.UsageCounter {
	if q.Unlimited || q.Limit == premiumuc.UnlimitedMonthly {
		return buildCounter(q.Used, 0, true)
	}
	return buildCounter(q.Used, q.Limit, false)
}

func quotaToFeatureDTO(q premiumuc.Quota) dto.FeatureAccessDTO {
	out := dto.FeatureAccessDTO{
		Allowed:       q.Allowed,
		Unlimited:     q.Unlimited,
		Used:          q.Used,
		Kind:          string(q.Kind),
		HistoryMonths: q.HistoryMonths,
	}
	if q.Unlimited || q.Limit == premiumuc.UnlimitedMonthly {
		out.Limit = 0
		out.Remaining = 0
	} else {
		out.Limit = q.Limit
		out.Remaining = q.Remaining
	}
	return out
}

// GetQuota returns the user's current plan and monthly counters (UTC month).
func (s *Service) GetQuota(ctx context.Context, userID uuid.UUID) (dto.UsageQuotaResponse, error) {
	var out dto.UsageQuotaResponse
	if userID == uuid.Nil {
		return out, fmt.Errorf("%w: user id required", ErrUnavailable)
	}
	if s == nil || s.gates == nil {
		return out, fmt.Errorf("%w", ErrUnavailable)
	}

	tier, err := s.planTier(ctx, userID)
	if err != nil {
		return out, err
	}
	paid := tier.IsPaidPlan()
	now := time.Now().UTC()
	out.PlanTier = string(tier)
	out.IsPremium = paid
	out.IsPremiumPlus = tier == domain.PlanPremiumPlus
	out.Period = now.Format("2006-01")

	suggestQ, err := s.gates.GetRemainingQuota(ctx, userID, domain.FeatureAIRoutineSuggestion)
	if err != nil {
		return out, err
	}
	editQ, err := s.gates.GetRemainingQuota(ctx, userID, domain.FeatureEditRoutine)
	if err != nil {
		return out, err
	}
	histQ, err := s.gates.GetRemainingQuota(ctx, userID, domain.FeatureProgressFullHistory)
	if err != nil {
		return out, err
	}

	out.Wardrobe = s.wardrobeUsage(ctx, userID, paid)
	out.RoutineSuggest = quotaToCounter(suggestQ)
	out.RoutineManualEdit = quotaToCounter(editQ)
	out.ProgressHistoryMonths = histQ.HistoryMonths

	out.Features = make(map[string]dto.FeatureAccessDTO, len(domain.AllFeatures))
	for _, f := range domain.AllFeatures {
		q, qErr := s.gates.GetRemainingQuota(ctx, userID, f)
		if qErr != nil {
			continue
		}
		out.Features[string(f)] = quotaToFeatureDTO(q)
	}
	// Override wardrobe_full with live shelf slots so Free create uses the
	// soft-paywall (allowed while remaining > 0). Manage stays Premium-only
	// via AssertWardrobeManage / wardrobe.can_manage.
	out.Features[string(domain.FeatureWardrobeFull)] = wardrobeFeatureDTO(out.Wardrobe)
	return out, nil
}

func (s *Service) shelfCount(ctx context.Context, userID uuid.UUID) int {
	if s == nil || s.shelf == nil || userID == uuid.Nil {
		return 0
	}
	n, err := s.shelf.CountByUser(ctx, userID)
	if err != nil || n < 0 {
		return 0
	}
	return int(n)
}

func (s *Service) wardrobeUsage(ctx context.Context, userID uuid.UUID, paid bool) dto.WardrobeUsage {
	used := s.shelfCount(ctx, userID)
	if paid {
		return dto.WardrobeUsage{
			CanWrite:  true,
			CanManage: true,
			Used:      used,
			Unlimited: true,
		}
	}
	remaining := FreeWardrobeProductLimit - used
	if remaining < 0 {
		remaining = 0
	}
	return dto.WardrobeUsage{
		CanWrite:  remaining > 0,
		CanManage: false,
		Used:      used,
		Limit:     FreeWardrobeProductLimit,
		Remaining: remaining,
	}
}

func wardrobeFeatureDTO(w dto.WardrobeUsage) dto.FeatureAccessDTO {
	if w.Unlimited {
		return dto.FeatureAccessDTO{
			Allowed:   true,
			Unlimited: true,
			Used:      w.Used,
			Kind:      "shelf_slots",
		}
	}
	return dto.FeatureAccessDTO{
		Allowed:   w.CanWrite,
		Unlimited: false,
		Used:      w.Used,
		Limit:     w.Limit,
		Remaining: w.Remaining,
		Kind:      "shelf_slots",
	}
}

// AssertWardrobeCreate allows Premium always, or Free while under FreeWardrobeProductLimit.
func (s *Service) AssertWardrobeCreate(ctx context.Context, userID uuid.UUID) error {
	if s == nil || s.gates == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	paid, err := s.IsPremium(ctx, userID)
	if err != nil {
		return err
	}
	if paid {
		return nil
	}
	if s.shelfCount(ctx, userID) >= FreeWardrobeProductLimit {
		return ErrQuotaExceeded
	}
	return nil
}

// AssertWardrobeManage requires wardrobe_full (Premium / Premium+) for edit/delete.
func (s *Service) AssertWardrobeManage(ctx context.Context, userID uuid.UUID) error {
	if s == nil || s.gates == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	return s.mapAssert(s.gates.AssertFeature(ctx, userID, domain.FeatureWardrobeFull))
}

// AssertWardrobeWrite is an alias for AssertWardrobeManage (edit/delete / full shelf).
func (s *Service) AssertWardrobeWrite(ctx context.Context, userID uuid.UUID) error {
	return s.AssertWardrobeManage(ctx, userID)
}

func (s *Service) mapAssert(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, premiumuc.ErrQuotaExceeded):
		return ErrQuotaExceeded
	case errors.Is(err, premiumuc.ErrFeatureDenied):
		return ErrPremiumRequired
	case errors.Is(err, premiumuc.ErrUnavailable):
		return ErrUnavailable
	default:
		return err
	}
}

// AssertRoutineSuggest checks the monthly AI suggest quota for free users.
func (s *Service) AssertRoutineSuggest(ctx context.Context, userID uuid.UUID) error {
	if s == nil || s.gates == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	return s.mapAssert(s.gates.AssertFeature(ctx, userID, domain.FeatureAIRoutineSuggestion))
}

// RecordRoutineSuggest increments the suggest counter after a successful AI call.
func (s *Service) RecordRoutineSuggest(ctx context.Context, userID uuid.UUID) error {
	if s == nil || s.gates == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	return s.mapAssert(s.gates.IncrementUsage(ctx, userID, domain.FeatureAIRoutineSuggestion))
}

// AssertRoutineManualEdit checks the monthly manual-edit quota for free users.
func (s *Service) AssertRoutineManualEdit(ctx context.Context, userID uuid.UUID) error {
	if s == nil || s.gates == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	return s.mapAssert(s.gates.AssertFeature(ctx, userID, domain.FeatureEditRoutine))
}

// RecordRoutineManualEdit increments the manual-edit counter after a structural save.
func (s *Service) RecordRoutineManualEdit(ctx context.Context, userID uuid.UUID) error {
	if s == nil || s.gates == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	return s.mapAssert(s.gates.IncrementUsage(ctx, userID, domain.FeatureEditRoutine))
}

// IsTickOnlySave returns true when the client only persisted completion ticks.
func IsTickOnlySave(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(kind), "tick_only")
}

// IsPreferenceOnlySave returns true when the client only updated skill_mode.
func IsPreferenceOnlySave(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(kind), "preference_only")
}
