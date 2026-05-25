// Package auth implements registration and login application logic (use cases).
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

// Service coordinates registration, login, and session token issuance.
type Service struct {
	users  UserReaderWriter
	tokens TokenIssuer
}

// NewService builds an auth Service with required ports.
func NewService(users UserReaderWriter, tokens TokenIssuer) *Service {
	return &Service{users: users, tokens: tokens}
}

// Result is returned by Register and Login (tokens + public user profile).
type Result struct {
	Tokens dto.AuthTokensResponse
	User   dto.UserPublic
}

// Register creates a local user with hashed password and returns JWT pair.
func (s *Service) Register(ctx context.Context, req dto.RegisterRequest) (Result, error) {
	var zero Result
	if s == nil || s.users == nil || s.tokens == nil {
		return zero, ErrTokenConfig
	}

	email := normalizeEmail(req.Email)
	password := strings.TrimSpace(req.Password)
	if email == "" || password == "" {
		return zero, fmt.Errorf("%w: email and password are required", ErrInvalidInput)
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return zero, fmt.Errorf("%w: invalid email", ErrInvalidInput)
	}
	if len(password) < minPasswordLen {
		return zero, fmt.Errorf("%w: password must be at least %d characters", ErrInvalidInput, minPasswordLen)
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

	existing, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return zero, fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	if existing != nil {
		return zero, ErrEmailTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return zero, fmt.Errorf("%w: hash password", ErrDatabase)
	}

	user := &domain.User{
		Email:        email,
		Username:     username,
		PasswordHash: string(hash),
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Provider:     domain.AuthProviderLocal,
		IsActive:     true,
	}

	if err := s.users.Create(ctx, user); err != nil {
		if repository.IsUniqueViolation(err) {
			// Race: email or username conflict.
			if u, _ := s.users.GetByEmail(ctx, email); u != nil {
				return zero, ErrEmailTaken
			}
			return zero, ErrUsernameTaken
		}
		return zero, fmt.Errorf("%w: %v", ErrDatabase, err)
	}

	return s.issueResult(user)
}

// Login validates credentials and returns JWT pair + public profile.
func (s *Service) Login(ctx context.Context, req dto.LoginRequest) (Result, error) {
	var zero Result
	if s == nil || s.users == nil || s.tokens == nil {
		return zero, ErrTokenConfig
	}

	email := normalizeEmail(req.Email)
	password := strings.TrimSpace(req.Password)
	if email == "" || password == "" {
		return zero, fmt.Errorf("%w: email and password are required", ErrInvalidInput)
	}

	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return zero, fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	if user == nil {
		return zero, ErrInvalidCredentials
	}
	if !user.IsActive {
		return zero, ErrUserInactive
	}
	if user.Provider != domain.AuthProviderLocal || user.PasswordHash == "" {
		return zero, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return zero, ErrInvalidCredentials
	}

	return s.issueResult(user)
}

// Me returns the public profile for a user ID (typically from JWT subject).
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (dto.UserPublic, error) {
	if s == nil || s.users == nil {
		return dto.UserPublic{}, ErrTokenConfig
	}
	if userID == uuid.Nil {
		return dto.UserPublic{}, fmt.Errorf("%w: missing user id", ErrInvalidInput)
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return dto.UserPublic{}, fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	if user == nil {
		return dto.UserPublic{}, ErrUserNotFound
	}
	return dto.UserFromDomain(user), nil
}

func (s *Service) issueResult(user *domain.User) (Result, error) {
	if user == nil {
		return Result{}, ErrUserNotFound
	}
	access, err := s.tokens.SignAccess(user.ID)
	if err != nil {
		return Result{}, fmt.Errorf("sign access token: %w", err)
	}
	refresh, err := s.tokens.SignRefresh(user.ID)
	if err != nil {
		return Result{}, fmt.Errorf("sign refresh token: %w", err)
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
		return fmt.Errorf("%w: username length must be between 3 and %d", ErrInvalidInput, maxUsernameLen)
	}
	for _, r := range username {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' {
			return fmt.Errorf("%w: username may only contain letters, numbers, and underscore", ErrInvalidInput)
		}
	}
	return nil
}

// ensureUniqueUsername appends a short suffix if the base username is taken.
func (s *Service) ensureUniqueUsername(ctx context.Context, base string) (string, error) {
	candidate := base
	for i := 0; i < 20; i++ {
		exists, err := s.users.UsernameExists(ctx, candidate)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrDatabase, err)
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
	return "", fmt.Errorf("%w: could not allocate a unique username", ErrInvalidInput)
}

// WrapDatabase maps generic DB errors for handlers.
func WrapDatabase(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrDatabase, err)
}
