// Package adminmetrics serves admin dashboard aggregates for Payment & Subscription.
package adminmetrics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
)

var (
	ErrUnavailable = errors.New("admin metrics unavailable")
)

const upcomingExpiryWindow = 7 * 24 * time.Hour

// PaymentMetricsQuery filters the recent-payments table on the metrics payload.
type PaymentMetricsQuery struct {
	Status string
	Limit  int
	Offset int
}

// Service aggregates payment / subscription health for admins.
type Service struct {
	orders *repository.PaymentOrderRepository
	users  *repository.GormUserRepository
	ops    *repository.PaymentOpsEventRepository
}

// NewService wires dependencies. Any nil core dep → ErrUnavailable.
func NewService(
	orders *repository.PaymentOrderRepository,
	users *repository.GormUserRepository,
	ops *repository.PaymentOpsEventRepository,
) *Service {
	return &Service{orders: orders, users: users, ops: ops}
}

func (s *Service) ready() error {
	if s == nil || s.orders == nil || s.users == nil {
		return ErrUnavailable
	}
	return nil
}

// PaymentMetrics returns today's payment KPIs + upcoming expiries (UTC day)
// plus a recent-payments table (optional status filter).
func (s *Service) PaymentMetrics(ctx context.Context, q PaymentMetricsQuery) (dto.AdminPaymentMetricsResponse, error) {
	var zero dto.AdminPaymentMetricsResponse
	if err := s.ready(); err != nil {
		return zero, err
	}

	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	stats, err := s.orders.AggregateCreatedBetween(ctx, dayStart, dayEnd)
	if err != nil {
		return zero, fmt.Errorf("aggregate payments: %w", err)
	}

	var successRate float64
	terminal := stats.PaidCount + stats.FailedCount
	if terminal > 0 {
		successRate = float64(stats.PaidCount) / float64(terminal) * 100
		// Round to 2 decimal places for a stable API.
		successRate = float64(int(successRate*100+0.5)) / 100
	}

	var webhookErrs int64
	if s.ops != nil {
		webhookErrs, err = s.ops.CountSince(ctx, domain.OpsKindWebhookError, now.Add(-24*time.Hour))
		if err != nil {
			return zero, fmt.Errorf("count webhook errors: %w", err)
		}
	}

	activePremium, err := s.users.CountActivePremiumUsers(ctx)
	if err != nil {
		return zero, fmt.Errorf("count premium: %w", err)
	}

	upcoming, err := s.users.ListUpcomingExpiries(ctx, now, upcomingExpiryWindow, 100)
	if err != nil {
		return zero, fmt.Errorf("upcoming expiries: %w", err)
	}
	expiryDTOs := make([]dto.AdminUpcomingExpiry, 0, len(upcoming))
	for _, u := range upcoming {
		expiryDTOs = append(expiryDTOs, dto.AdminUpcomingExpiry{
			UserID:        u.UserID.String(),
			Email:         u.Email,
			Plan:          string(u.PlanTier),
			PlanExpiresAt: u.PlanExpiresAt.Format(time.RFC3339),
		})
	}

	status := normalizePaymentStatusFilter(q.Status)
	limit := q.Limit
	if limit < 1 {
		limit = 50
	}
	recent, recentTotal, err := s.orders.ListRecent(ctx, repository.PaymentOrderListFilter{
		Status: status,
		Limit:  limit,
		Offset: q.Offset,
	})
	if err != nil {
		return zero, fmt.Errorf("list recent payments: %w", err)
	}
	recentDTOs := make([]dto.AdminPaymentOrderRow, 0, len(recent))
	for i := range recent {
		o := recent[i]
		row := dto.AdminPaymentOrderRow{
			ID:              o.ID.String(),
			UserID:          o.UserID.String(),
			InvoiceNumber:   o.InvoiceNumber,
			Plan:            string(o.PlanTier),
			BillingInterval: string(o.BillingInterval),
			AmountVND:       o.AmountVND,
			Status:          string(o.Status),
			Provider:        string(o.Provider),
			CreatedAt:       o.CreatedAt.UTC().Format(time.RFC3339),
		}
		if o.PaidAt != nil {
			row.PaidAt = o.PaidAt.UTC().Format(time.RFC3339)
		}
		recentDTOs = append(recentDTOs, row)
	}

	return dto.AdminPaymentMetricsResponse{
		TodayPayments:        stats.TotalCreated,
		SuccessRate:          successRate,
		TotalRevenue:         stats.RevenueVND,
		FailedCount:          stats.FailedCount,
		WebhookErrorsLast24h: webhookErrs,
		ActivePremiumCount:   activePremium,
		UpcomingExpiries:     expiryDTOs,
		RecentPayments:       recentDTOs,
		RecentPaymentsTotal:  recentTotal,
		AsOf:                 now.Format(time.RFC3339),
	}, nil
}

func normalizePaymentStatusFilter(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch domain.PaymentOrderStatus(s) {
	case domain.PaymentPending, domain.PaymentPaid, domain.PaymentFailed,
		domain.PaymentCancelled, domain.PaymentExpired:
		return s
	default:
		return ""
	}
}
