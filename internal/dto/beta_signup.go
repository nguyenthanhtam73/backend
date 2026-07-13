package dto

import (
	"net/mail"
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

const maxBetaSignupEmailRunes = 320

// CreateBetaSignupRequest is POST /api/v1/beta-signups.
type CreateBetaSignupRequest struct {
	Email string `json:"email"`
}

// BetaSignupCreateResponse is returned after a successful waitlist signup.
type BetaSignupCreateResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// ValidateAndMap converts the public signup DTO to a domain row or returns an error message.
func (r CreateBetaSignupRequest) ValidateAndMap() (*domain.BetaSignup, string) {
	email := normalizeBetaSignupEmail(r.Email)
	if email == "" {
		return nil, "email is required"
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, "email must be a valid address"
	}
	if runes := []rune(email); len(runes) > maxBetaSignupEmailRunes {
		return nil, "email is too long"
	}
	return &domain.BetaSignup{
		Email:  email,
		Source: domain.DefaultBetaSignupSource,
	}, ""
}

func normalizeBetaSignupEmail(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}
