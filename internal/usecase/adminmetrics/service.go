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
	orders    *repository.PaymentOrderRepository
	users     *repository.GormUserRepository
	ops       *repository.PaymentOpsEventRepository
	affiliate *repository.GormAffiliateClickRepository
}

// NewService wires dependencies. Any nil core dep → ErrUnavailable.
func NewService(
	orders *repository.PaymentOrderRepository,
	users *repository.GormUserRepository,
	ops *repository.PaymentOpsEventRepository,
) *Service {
	return &Service{orders: orders, users: users, ops: ops}
}

// WithAffiliate attaches affiliate click metrics (optional).
func (s *Service) WithAffiliate(clicks *repository.GormAffiliateClickRepository) *Service {
	if s == nil {
		return s
	}
	s.affiliate = clicks
	return s
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

// AffiliateMetrics returns click totals + top SKUs (7d / 30d / all-time per row).
func (s *Service) AffiliateMetrics(ctx context.Context, limit int) (dto.AdminAffiliateMetricsResponse, error) {
	var zero dto.AdminAffiliateMetricsResponse
	if s == nil || s.affiliate == nil {
		return zero, ErrUnavailable
	}
	if limit <= 0 {
		limit = 50
	}
	now := time.Now().UTC()
	since7 := now.Add(-7 * 24 * time.Hour)
	since30 := now.Add(-30 * 24 * time.Hour)

	c7, err := s.affiliate.CountSince(ctx, since7)
	if err != nil {
		return zero, fmt.Errorf("count affiliate 7d: %w", err)
	}
	c30, err := s.affiliate.CountSince(ctx, since30)
	if err != nil {
		return zero, fmt.Errorf("count affiliate 30d: %w", err)
	}
	ctotal, err := s.affiliate.CountAll(ctx)
	if err != nil {
		return zero, fmt.Errorf("count affiliate total: %w", err)
	}

	// Rank by 30d, then overlay 7d counts for the same SKU keys.
	rows30, err := s.affiliate.AggregateBySKUSince(ctx, since30, limit)
	if err != nil {
		return zero, fmt.Errorf("aggregate affiliate 30d: %w", err)
	}
	rows7, err := s.affiliate.AggregateBySKUSince(ctx, since7, 200)
	if err != nil {
		return zero, fmt.Errorf("aggregate affiliate 7d: %w", err)
	}
	map7 := make(map[string]int64, len(rows7))
	for _, r := range rows7 {
		map7[skuKey(r.ProductName, r.Brand, r.AffiliateLink)] = r.Clicks
	}

	top := make([]dto.AdminAffiliateSKURow, 0, len(rows30))
	for _, r := range rows30 {
		id, name, brand, ok := lookupCatalogSKU(r.AffiliateLink, r.ProductName, r.Brand)
		row := dto.AdminAffiliateSKURow{
			ProductID:     id,
			ProductName:   name,
			Brand:         brand,
			AffiliateLink: r.AffiliateLink,
			Clicks7d:      map7[skuKey(r.ProductName, r.Brand, r.AffiliateLink)],
			Clicks30d:     r.Clicks,
			ClicksTotal:   r.Clicks, // within window used for ranking (30d); total window below
			LastClickAt:   r.LastClickAt.UTC().Format(time.RFC3339),
		}
		if !ok {
			row.ProductName = r.ProductName
			row.Brand = r.Brand
		}
		top = append(top, row)
	}

	// Best-effort total-all-time per shown SKU via a second aggregate from epoch.
	rowsAll, err := s.affiliate.AggregateBySKUSince(ctx, time.Unix(0, 0).UTC(), 200)
	if err == nil {
		mapAll := make(map[string]int64, len(rowsAll))
		for _, r := range rowsAll {
			mapAll[skuKey(r.ProductName, r.Brand, r.AffiliateLink)] = r.Clicks
		}
		for i := range top {
			k := skuKey(top[i].ProductName, top[i].Brand, top[i].AffiliateLink)
			// Prefer original click-row key fields — rebuild from 30d row
			k = skuKey(rows30[i].ProductName, rows30[i].Brand, rows30[i].AffiliateLink)
			if n, ok := mapAll[k]; ok {
				top[i].ClicksTotal = n
			}
		}
	}

	return dto.AdminAffiliateMetricsResponse{
		Clicks7d:    c7,
		Clicks30d:   c30,
		ClicksTotal: ctotal,
		TopSKUs:     top,
		AsOf:        now.Format(time.RFC3339),
	}, nil
}

func skuKey(name, brand, link string) string {
	return strings.ToLower(strings.TrimSpace(name)) + "|" +
		strings.ToLower(strings.TrimSpace(brand)) + "|" +
		strings.TrimSpace(link)
}

// lookupCatalogSKU is implemented in affiliate_lookup.go to avoid importing ai from usecase tests lightly.
// Defined as a package-level func var for testability.
var lookupCatalogSKU = defaultLookupCatalogSKU

func defaultLookupCatalogSKU(link, fallbackName, fallbackBrand string) (id, name, brand string, ok bool) {
	return "", fallbackName, fallbackBrand, false
}
