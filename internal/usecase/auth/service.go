// Package auth implements AuthUsecase: register, login, logout, getMe.
//
// Layering: Domain → AuthRepository → AuthUsecase → Handler.
package auth

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
)

const (
	minPasswordLen = 8
	maxUsernameLen = 64
	bcryptCost     = 12
)

var usernameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

// Service is the AuthUsecase implementation.
type Service struct {
	repo   AuthRepository
	tokens TokenIssuer
}

// Usecase is an alias for Service (Clean Architecture naming).
type Usecase = Service

// NewService / NewUsecase builds AuthUsecase with injected repository + token issuer.
func NewService(repo AuthRepository, tokens TokenIssuer) *Service {
	return &Service{repo: repo, tokens: tokens}
}

// NewUsecase is the preferred constructor name for DI wiring.
func NewUsecase(repo AuthRepository, tokens TokenIssuer) *Usecase {
	return NewService(repo, tokens)
}

// Result is returned by Register and Login (tokens + public user profile).
type Result struct {
	Tokens dto.AuthTokensResponse
	User   dto.UserPublic
}

// Register creates a local user with hashed password and returns JWT pair.
func (s *Service) Register(ctx context.Context, req dto.RegisterRequest) (Result, error) {
	var zero Result
	if s == nil || s.repo == nil || s.tokens == nil {
		return zero, appTokenConfig()
	}

	email := normalizeEmail(req.Email)
	password := strings.TrimSpace(req.Password)
	if email == "" || password == "" {
		return zero, appInvalidInput("email and password are required")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return zero, appInvalidInput("invalid email")
	}
	if len(password) < minPasswordLen {
		return zero, appInvalidInput(fmt.Sprintf("password must be at least %d characters", minPasswordLen))
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = deriveUsername(email)
	}
	if err := validateUsername(username); err != nil {
		return zero, err
	}
	username, err := s.ensureUniqueUsername(ctx, username)
	if err != nil {
		return zero, err
	}

	existing, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		return zero, appDatabase(err)
	}
	if existing != nil {
		return zero, appEmailTaken()
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return zero, appDatabase(err)
	}

	user := &domain.User{
		Email:        email,
		Username:     username,
		PasswordHash: string(hash),
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Provider:     domain.AuthProviderLocal,
		IsActive:     true,
	}

	if err := s.repo.Create(ctx, user); err != nil {
		if repository.IsUniqueViolation(err) {
			if u, _ := s.repo.GetByEmail(ctx, email); u != nil {
				return zero, appEmailTaken()
			}
			return zero, appUsernameTaken()
		}
		return zero, appDatabase(err)
	}

	return s.issueResult(user)
}

// Login validates credentials and returns JWT pair + public profile.
func (s *Service) Login(ctx context.Context, req dto.LoginRequest) (Result, error) {
	var zero Result
	if s == nil || s.repo == nil || s.tokens == nil {
		return zero, appTokenConfig()
	}

	email := normalizeEmail(req.Email)
	password := strings.TrimSpace(req.Password)
	if email == "" || password == "" {
		return zero, appInvalidInput("email and password are required")
	}

	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		return zero, appDatabase(err)
	}
	if user == nil {
		return zero, appInvalidCredentials()
	}
	if !user.IsActive {
		return zero, appUserInactive()
	}
	if user.Provider != domain.AuthProviderLocal || user.PasswordHash == "" {
		return zero, appInvalidCredentials()
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return zero, appInvalidCredentials()
	}

	return s.issueResult(user)
}

// Me (GetMe) returns the public profile for a user ID (JWT subject).
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (dto.UserPublic, error) {
	if s == nil || s.repo == nil {
		return dto.UserPublic{}, appTokenConfig()
	}
	if userID == uuid.Nil {
		return dto.UserPublic{}, appInvalidInput("missing user id")
	}

	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return dto.UserPublic{}, appDatabase(err)
	}
	if user == nil {
		return dto.UserPublic{}, appUserNotFound()
	}
	return dto.UserFromDomain(user), nil
}

// GetMe is an alias for Me (Clean Architecture naming).
func (s *Service) GetMe(ctx context.Context, userID uuid.UUID) (dto.UserPublic, error) {
	return s.Me(ctx, userID)
}

// Logout acknowledges client-side session clear (stateless JWT — no server revoke list yet).
func (s *Service) Logout(_ context.Context, userID uuid.UUID) error {
	if s == nil {
		return appTokenConfig()
	}
	if userID == uuid.Nil {
		return appInvalidInput("missing user id")
	}
	return nil
}

func (s *Service) issueResult(user *domain.User) (Result, error) {
	if user == nil {
		return Result{}, appUserNotFound()
	}
	access, err := s.tokens.SignAccess(user.ID)
	if err != nil {
		return Result{}, domain.Internal("token_error", "could not issue access token", err)
	}
	refresh, err := s.tokens.SignRefresh(user.ID)
	if err != nil {
		return Result{}, domain.Internal("token_error", "could not issue refresh token", err)
	}
	exp := int64(s.tokens.AccessTTL().Seconds())
	if exp < 1 {
		exp = 1
	}
	return Result{
		Tokens: dto.AuthTokensResponse{
			AccessToken:  access,
			RefreshToken: refresh,
			TokenType:    "Bearer",
			ExpiresIn:    exp,
		},
		User: dto.UserFromDomain(user),
	}, nil
}

func normalizeEmail(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

func deriveUsername(email string) string {
	at := strings.LastIndex(email, "@")
	local := email
	if at > 0 {
		local = email[:at]
	}
	local = usernameSanitizer.ReplaceAllString(local, "")
	if local == "" {
		local = "user"
	}
	if len(local) > maxUsernameLen {
		local = local[:maxUsernameLen]
	}
	return local
}

func validateUsername(username string) error {
	if len(username) < 3 || len(username) > maxUsernameLen {
		return appInvalidInput(fmt.Sprintf("username length must be between 3 and %d", maxUsernameLen))
	}
	for _, r := range username {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' {
			return appInvalidInput("username may only contain letters, numbers, and underscore")
		}
	}
	return nil
}

func (s *Service) ensureUniqueUsername(ctx context.Context, base string) (string, error) {
	candidate := base
	for i := 0; i < 20; i++ {
		exists, err := s.repo.UsernameExists(ctx, candidate)
		if err != nil {
			return "", appDatabase(err)
		}
		if !exists {
			return candidate, nil
		}
		suffix := uuid.New().String()[:8]
		trim := maxUsernameLen - len(suffix) - 1
		if trim < 3 {
			trim = 3
		}
		prefix := base
		if len(prefix) > trim {
			prefix = prefix[:trim]
		}
		candidate = prefix + "_" + suffix
	}
	return "", appInvalidInput("could not allocate a unique username")
}

// WrapDatabase maps generic DB errors for handlers (legacy helper).
func WrapDatabase(err error) error {
	if err == nil {
		return nil
	}
	return appDatabase(err)
}
