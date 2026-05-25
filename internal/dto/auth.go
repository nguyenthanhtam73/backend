package dto

import "github.com/dadiary/backend/internal/domain"

// RegisterRequest is the JSON body for POST /api/v1/auth/register.
type RegisterRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	Username       string `json:"username,omitempty"` // optional; derived from email if empty
	DisplayName    string `json:"display_name,omitempty"`
	TurnstileToken string `json:"turnstile_token,omitempty"` // Cloudflare Turnstile widget token when captcha enabled
}

// LoginRequest is the JSON body for POST /api/v1/auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// AuthTokensResponse is returned after successful register or login.
type AuthTokensResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"` // seconds until access token expiry
}

// UserPublic is a safe projection of domain.User for API responses.
type UserPublic struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Provider    string `json:"provider"`
	IsActive    bool   `json:"is_active"`
	CreatedAt   string `json:"created_at"`
}

// UserFromDomain maps a domain user to a public DTO (no secrets).
func UserFromDomain(u *domain.User) UserPublic {
	if u == nil {
		return UserPublic{}
	}
	return UserPublic{
		ID:          u.ID.String(),
		Email:       u.Email,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarURL,
		Provider:    string(u.Provider),
		IsActive:    u.IsActive,
		CreatedAt:   u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}
