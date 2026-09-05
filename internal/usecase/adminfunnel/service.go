package adminfunnel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
)

var (
	ErrUnavailable = errors.New("admin funnel stats unavailable")
)

const (
	paywallNote  = "N/A — paywall is client-only and is not persisted in Postgres"
	calendarNote = "Asia/Ho_Chi_Minh"
	d0Note       = "Users with a skin_check.check_date on their Vietnam signup day"
	d1Note       = "Users with a skin_check.check_date on the Vietnam day after signup. d1_eligible_users is the denominator (signup day strictly before today VN)."
	windowsNote  = "1d/7d signup, skin-check, and paid-order windows are rolling hours from as_of. D0/D1 use Vietnam civil dates."
)

// Service aggregates leaky-bucket proxies for admins.
type Service struct {
	users  *repository.GormUserRepository
	checks *repository.GormSkinCheckRepository
	orders *repository.PaymentOrderRepository
	now    func() time.Time
}

// NewService wires repositories. Any nil dep → ErrUnavailable.
func NewService(
	users *repository.GormUserRepository,
	checks *repository.GormSkinCheckRepository,
	orders *repository.PaymentOrderRepository,
) *Service {
	return &Service{users: users, checks: checks, orders: orders, now: time.Now}
}

func (s *Service) ready() error {
	if s == nil || s.users == nil || s.checks == nil || s.orders == nil {
		return ErrUnavailable
	}
	return nil
}

// Stats returns read-only funnel counts at now (UTC timestamp on as_of).
func (s *Service) Stats(ctx context.Context) (dto.AdminFunnelStatsResponse, error) {
	var zero dto.AdminFunnelStatsResponse
	if err := s.ready(); err != nil {
		return zero, err
	}
	nowFn := s.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	since1d := now.Add(-24 * time.Hour)
	since7d := now.Add(-7 * 24 * time.Hour)

	signed1d, err := s.users.CountCreatedSince(ctx, since1d)
	if err != nil {
		return zero, fmt.Errorf("count signed up 1d: %w", err)
	}
	signed7d, err := s.users.CountCreatedSince(ctx, since7d)
	if err != nil {
		return zero, fmt.Errorf("count signed up 7d: %w", err)
	}

	checkEver, err := s.checks.CountDistinctUsers(ctx)
	if err != nil {
		return zero, fmt.Errorf("count skin-check users ever: %w", err)
	}
	check1d, err := s.checks.CountDistinctUsersCreatedSince(ctx, since1d)
	if err != nil {
		return zero, fmt.Errorf("count skin-check users 1d: %w", err)
	}
	check7d, err := s.checks.CountDistinctUsersCreatedSince(ctx, since7d)
	if err != nil {
		return zero, fmt.Errorf("count skin-check users 7d: %w", err)
	}

	paid7d, err := s.orders.CountPaidSince(ctx, since7d)
	if err != nil {
		return zero, fmt.Errorf("count paid orders 7d: %w", err)
	}

	signups, err := s.users.ListFunnelSignups(ctx)
	if err != nil {
		return zero, fmt.Errorf("list signups: %w", err)
	}
	checkDates, err := s.checks.ListFunnelCheckDates(ctx)
	if err != nil {
		return zero, fmt.Errorf("list check dates: %w", err)
	}

	signupRows := make([]SignupRow, 0, len(signups))
	for _, u := range signups {
		signupRows = append(signupRows, SignupRow{UserID: u.ID, CreatedAt: u.CreatedAt})
	}
	checkRows := make([]CheckDateRow, 0, len(checkDates))
	for _, c := range checkDates {
		checkRows = append(checkRows, CheckDateRow{UserID: c.UserID, CheckDate: c.CheckDate})
	}
	proxies := CountRetentionProxies(signupRows, checkRows, now, since7d)

	return dto.AdminFunnelStatsResponse{
		SignedUp1d:         signed1d,
		SignedUp7d:         signed7d,
		SkinCheckUsersEver: checkEver,
		SkinCheckUsers1d:   check1d,
		SkinCheckUsers7d:   check7d,
		D0CheckinUsers:     proxies.D0Users,
		D0CheckinUsers7d:   proxies.D0Users7d,
		D1CheckinUsers:     proxies.D1Users,
		D1EligibleUsers:    proxies.D1Eligible,
		D1CheckinUsers7d:   proxies.D1Users7d,
		D1EligibleUsers7d:  proxies.D1Eligible7d,
		PaidOrders7d:       paid7d,
		PaywallViews:       nil,
		Notes: dto.AdminFunnelNotes{
			Paywall:  paywallNote,
			Calendar: calendarNote,
			D0:       d0Note,
			D1:       d1Note,
			Windows:  windowsNote,
		},
		AsOf: now.UTC().Format(time.RFC3339),
	}, nil
}
