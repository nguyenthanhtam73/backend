// Package betasignup handles public Beta waitlist email signups from the landing page.
package betasignup

import (
	"context"
	"errors"
	"fmt"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
)

var (
	ErrUnavailable            = errors.New("beta signup service unavailable")
	ErrEmailAlreadyRegistered = errors.New("email already registered for beta")
)

// Service persists Beta waitlist signups.
type Service struct {
	repo *repository.GormBetaSignupRepository
}

// NewService constructs Service.
func NewService(repo *repository.GormBetaSignupRepository) *Service {
	return &Service{repo: repo}
}

// Create stores one Beta waitlist signup from the public landing form.
func (s *Service) Create(
	ctx context.Context,
	req dto.CreateBetaSignupRequest,
) (dto.BetaSignupCreateResponse, error) {
	var zero dto.BetaSignupCreateResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}

	row, msg := req.ValidateAndMap()
	if row == nil {
		return zero, fmt.Errorf("%s", msg)
	}

	existing, err := s.repo.GetByEmail(ctx, row.Email)
	if err != nil {
		return zero, err
	}
	if existing != nil {
		return zero, ErrEmailAlreadyRegistered
	}

	if err := s.repo.Create(ctx, row); err != nil {
		if repository.IsUniqueViolation(err) {
			return zero, ErrEmailAlreadyRegistered
		}
		return zero, err
	}

	return dto.BetaSignupCreateResponse{
		ID:      row.ID.String(),
		Message: "beta_signup_success",
	}, nil
}
