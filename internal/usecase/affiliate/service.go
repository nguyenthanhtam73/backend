// Package affiliate logs affiliate product link clicks from the coach UI.
package affiliate

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

var ErrUnavailable = errors.New("affiliate service unavailable")

// Service persists affiliate click events.
type Service struct {
	clicks *repository.GormAffiliateClickRepository
}

// NewService wires dependencies.
func NewService(clicks *repository.GormAffiliateClickRepository) *Service {
	return &Service{clicks: clicks}
}

// LogClick records one affiliate link tap.
func (s *Service) LogClick(ctx context.Context, userID uuid.UUID, req dto.LogAffiliateClickRequest) (dto.AffiliateClickResponse, error) {
	var zero dto.AffiliateClickResponse
	if s == nil || s.clicks == nil {
		return zero, ErrUnavailable
	}
	row, msg := req.ValidateAndMap(userID)
	if row == nil {
		return zero, fmt.Errorf("%s", msg)
	}
	if err := s.clicks.Create(ctx, row); err != nil {
		return zero, err
	}
	slog.Info("affiliate_click",
		"user_id", userID.String(),
		"source", row.Source,
		"product", row.Brand+" — "+row.ProductName,
		"context_id", row.ContextID,
	)
	return dto.AffiliateClickResponse{
		ID:       row.ID.String(),
		LoggedAt: row.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}
