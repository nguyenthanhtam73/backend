// Package token issues and validates JWT access/refresh tokens for the API.
package token

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	claimTokenUseAccess  = "access"
	claimTokenUseRefresh = "refresh"
)

// ErrInvalidToken is returned when a token string cannot be parsed or fails validation.
var ErrInvalidToken = errors.New("invalid token")

// Service signs and parses HS256 JWTs using application JWT config.
type Service struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewService validates config and constructs a token Service.
func NewService(cfg config.JWTConfig) (*Service, error) {
	if strings.TrimSpace(cfg.Secret) == "" {
		return nil, fmt.Errorf("jwt secret is empty: set jwt.secret or DADIARY_JWT_SECRET")
	}
	if cfg.AccessTTL <= 0 || cfg.RefreshTTL <= 0 {
		return nil, fmt.Errorf("jwt ttl must be positive")
	}
	return &Service{
		secret:     []byte(cfg.Secret),
		accessTTL:  cfg.AccessTTL,
		refreshTTL: cfg.RefreshTTL,
	}, nil
}

// diaryClaims embeds registered JWT claims plus a token_use discriminator.
type diaryClaims struct {
	TokenUse string `json:"token_use"`
	jwt.RegisteredClaims
}

// AccessTTL returns configured access token lifetime.
func (s *Service) AccessTTL() time.Duration {
	return s.accessTTL
}

// SignAccess creates a short-lived access JWT for the given user ID.
func (s *Service) SignAccess(userID uuid.UUID) (string, error) {
	return s.sign(userID, claimTokenUseAccess, s.accessTTL)
}

// SignRefresh creates a long-lived refresh JWT for the given user ID.
func (s *Service) SignRefresh(userID uuid.UUID) (string, error) {
	return s.sign(userID, claimTokenUseRefresh, s.refreshTTL)
}

func (s *Service) sign(userID uuid.UUID, use string, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := diaryClaims{
		TokenUse: use,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims)
	return t.SignedString(s.secret)
}

// ParseAccessToken validates an access JWT and returns the embedded user ID.
func (s *Service) ParseAccessToken(tokenString string) (uuid.UUID, error) {
	return s.parse(tokenString, claimTokenUseAccess)
}

// ParseRefreshToken validates a refresh JWT and returns the embedded user ID.
func (s *Service) ParseRefreshToken(tokenString string) (uuid.UUID, error) {
	return s.parse(tokenString, claimTokenUseRefresh)
}

func (s *Service) parse(tokenString, expectedUse string) (uuid.UUID, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return uuid.Nil, ErrInvalidToken
	}

	parsed, err := jwt.ParseWithClaims(tokenString, &diaryClaims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := parsed.Claims.(*diaryClaims)
	if !ok || !parsed.Valid {
		return uuid.Nil, ErrInvalidToken
	}
	if claims.TokenUse != expectedUse {
		return uuid.Nil, ErrInvalidToken
	}

	id, err := uuid.Parse(strings.TrimSpace(claims.Subject))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, ErrInvalidToken
	}
	return id, nil
}
