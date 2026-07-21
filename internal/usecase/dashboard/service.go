// Package dashboard aggregates home-screen summary data (Clean Architecture usecase).
package dashboard

import (
	"context"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/streaktime"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	streakuc "github.com/dadiary/backend/internal/usecase/streak"
	usageuc "github.com/dadiary/backend/internal/usecase/usage"
	"github.com/google/uuid"
)

// SkinCheckLister loads check-ins for progress summary aggregation.
type SkinCheckLister interface {
	ListForOwner(ctx context.Context, userID uuid.UUID, since time.Time, limit int) ([]domain.SkinCheck, error)
}

// Service is the DashboardUsecase.
type Service struct {
	streak   *streakuc.Service
	usage    *usageuc.Service
	checks   SkinCheckLister
	premium  *premiumuc.Service
}

// Usecase alias for DI clarity.
type Usecase = Service

// NewService / NewUsecase wires dashboard dependencies.
func NewService(
	streak *streakuc.Service,
	usage *usageuc.Service,
	checks SkinCheckLister,
	premium *premiumuc.Service,
) *Service {
	return &Service{streak: streak, usage: usage, checks: checks, premium: premium}
}

func NewUsecase(
	streak *streakuc.Service,
	usage *usageuc.Service,
	checks SkinCheckLister,
	premium *premiumuc.Service,
) *Usecase {
	return NewService(streak, usage, checks, premium)
}

// GetSummary returns streak + usage quotas + recent progress summary for the home dashboard.
func (s *Service) GetSummary(ctx context.Context, userID uuid.UUID) (dto.DashboardSummaryResponse, error) {
	var zero dto.DashboardSummaryResponse
	if s == nil || s.streak == nil || s.usage == nil || s.checks == nil {
		return zero, domain.Unavailable("service_unavailable", "dashboard service unavailable")
	}
	if userID == uuid.Nil {
		return zero, domain.BadRequest("invalid_input", "missing user id")
	}

	streakRes, err := s.streak.Get(ctx, userID)
	if err != nil {
		return zero, domain.Wrap(err, 503, "streak_unavailable", "could not load streak")
	}

	usageRes, err := s.usage.GetQuota(ctx, userID)
	if err != nil {
		return zero, domain.Wrap(err, 500, "usage_error", "could not load usage")
	}

	rangeDays, since := s.clampProgressRange(ctx, userID, 30)
	rows, err := s.checks.ListForOwner(ctx, userID, since, 0)
	if err != nil {
		return zero, domain.Wrap(err, 503, "database_error", "could not load progress")
	}
	full := dto.NewProgressTimelineResponse(rows, rangeDays, "")

	return dto.DashboardSummaryResponse{
		PlanTier: usageRes.PlanTier,
		Streak:   streakRes,
		Usage:    usageRes,
		Progress: dto.DashboardProgress{
			RangeDays: full.RangeDays,
			From:      full.From,
			To:        full.To,
			Summary:   full.Summary,
		},
	}, nil
}

func (s *Service) clampProgressRange(ctx context.Context, userID uuid.UUID, requestedDays int) (int, time.Time) {
	today := streaktime.Today()
	months := 3
	if s.premium != nil {
		m, err := s.premium.ProgressHistoryMonths(ctx, userID)
		if err == nil {
			months = m
		}
	}
	days, since := premiumuc.ClampProgressRange(months, requestedDays, today)
	return days, since
}
