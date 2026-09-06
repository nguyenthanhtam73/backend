// Package paywallview persists client paywall / upsell / upgrade impressions.
package paywallview

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

var ErrUnavailable = errors.New("paywall view service unavailable")

// Service records paywall impressions for founder funnel stats.
type Service struct {
	views *repository.GormPaywallViewRepository
}

// NewService wires dependencies.
func NewService(views *repository.GormPaywallViewRepository) *Service {
	return &Service{views: views}
}

// Log records one impression. Guests (uuid.Nil) are stored without a user_id.
func (s *Service) Log(ctx context.Context, userID uuid.UUID, req dto.LogPaywallViewRequest) (dto.PaywallViewResponse, error) {
	var zero dto.PaywallViewResponse
	if s == nil || s.views == nil {
		return zero, ErrUnavailable
	}
	row, msg := req.ValidateAndMap(userID)
	if row == nil {
		return zero, fmt.Errorf("%s", msg)
	}
	if err := s.views.Create(ctx, row); err != nil {
		return zero, err
	}
	slog.Info("paywall_view",
		"user_id", userID.String(),
		"surface", row.Surface,
		"feature", row.Feature,
	)
	return dto.PaywallViewResponse{
		ID:       row.ID.String(),
		LoggedAt: row.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}
