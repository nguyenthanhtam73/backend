// Package auth implements AuthUsecase: register, login, logout, refresh, getMe.
//
// Layering: Domain → AuthRepository → AuthUsecase → Handler.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
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
	repo     AuthRepository
	sessions RefreshSessionStore // optional in unit tests; required for revoke/refresh in prod
	tokens   TokenIssuer
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

// AttachSessions wires refresh-session persistence (logout revoke + /auth/refresh).
func (s *Service) AttachSessions(sessions RefreshSessionStore) {
	if s != nil {
		s.sessions = sessions
	}
}

// Result is returned by Register, Login, and Refresh (tokens + public user profile).
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
	if !validAccountEmail(email) {
		return zero, appInvalidEmail()
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
	// First-touch: persist the campaign values sent with this signup.
	// Email-taken rejects a second register and leaves the original row.
	req.Attribution.Apply(user)

	if err := s.repo.Create(ctx, user); err != nil {
		if repository.IsUniqueViolation(err) {
			if u, _ := s.repo.GetByEmail(ctx, email); u != nil {
				return zero, appEmailTaken()
			}
			return zero, appUsernameTaken()
		}
		return zero, appDatabase(err)
	}

	return s.issueResult(ctx, user, domain.NormalizeRefreshClient(req.Client))
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

	return s.issueResult(ctx, user, domain.NormalizeRefreshClient(req.Client))
}

// Refresh rotates a valid refresh token into a new access + refresh pair.
// clientHint is the marker from this request (header or body). The new session
// keeps the client kind already stored on the presented session, so an Android
// 90-day window slides forward on each use. clientHint is used only when the
// stored kind is empty.
func (s *Service) Refresh(ctx context.Context, refreshToken, clientHint string) (Result, error) {
	var zero Result
	if s == nil || s.repo == nil || s.tokens == nil {
		return zero, appTokenConfig()
	}
	raw := strings.TrimSpace(refreshToken)
	if raw == "" {
		return zero, appInvalidRefresh()
	}

	userID, jti, err := s.tokens.ParseRefreshToken(raw)
	if err != nil || userID == uuid.Nil || jti == uuid.Nil {
		return zero, appInvalidRefresh()
	}
	// Refresh requires server-side session tracking — fail closed when unwired.
	if s.sessions == nil {
		return zero, appTokenConfig()
	}

	now := time.Now().UTC()
	sess, err := s.sessions.GetByID(ctx, jti)
	if err != nil {
		return zero, appDatabase(err)
	}
	if sess == nil || sess.UserID != userID || !sess.IsActive(now) {
		return zero, appInvalidRefresh()
	}
	if sess.TokenHash != hashToken(raw) {
		return zero, appInvalidRefresh()
	}

	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return zero, appDatabase(err)
	}
	if user == nil {
		return zero, appUserNotFound()
	}
	if !user.IsActive {
		return zero, appUserInactive()
	}

	// Issue the new pair FIRST so a DB blip after revoke can't lock the user out.
	kind := strings.TrimSpace(sess.ClientKind)
	if kind == "" {
		kind = clientHint
	}
	out, err := s.issueResult(ctx, user, kind)
	if err != nil {
		return zero, err
	}
	// Best-effort revoke of the presented jti (rotation). Failure here still
	// returns the new tokens — old token remains valid until TTL / next use.
	_ = s.sessions.RevokeByID(ctx, jti, now)
	return out, nil
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

// Logout revokes one refresh session when refreshToken belongs to userID.
// An empty refreshToken revokes every session for the user (web clients that
// omit the token). A token that does not belong to userID is ignored.
// Access JWTs remain valid until expiry (stateless); clients must drop them.
func (s *Service) Logout(ctx context.Context, userID uuid.UUID, refreshToken string) error {
	if s == nil {
		return appTokenConfig()
	}
	if userID == uuid.Nil {
		return appInvalidInput("missing user id")
	}
	if s.sessions == nil {
		return nil
	}
	now := time.Now().UTC()
	if raw := strings.TrimSpace(refreshToken); raw != "" {
		return s.revokeOwnedSession(ctx, userID, raw, now)
	}
	return s.revokeAll(ctx, userID, now)
}

// LogoutAll revokes every refresh session for the user.
func (s *Service) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	if s == nil {
		return appTokenConfig()
	}
	if userID == uuid.Nil {
		return appInvalidInput("missing user id")
	}
	if s.sessions == nil {
		return nil
	}
	return s.revokeAll(ctx, userID, time.Now().UTC())
}

func (s *Service) revokeAll(ctx context.Context, userID uuid.UUID, at time.Time) error {
	if err := s.sessions.RevokeAllForUser(ctx, userID, at); err != nil {
		return appDatabase(err)
	}
	return nil
}

// revokeOwnedSession revokes the session for raw when it belongs to userID.
// Anything else (bad token, other user, unknown jti) is ignored.
func (s *Service) revokeOwnedSession(ctx context.Context, userID uuid.UUID, raw string, at time.Time) error {
	if s.tokens == nil {
		return nil
	}
	tokenUser, jti, err := s.tokens.ParseRefreshToken(raw)
	if err != nil || jti == uuid.Nil || tokenUser != userID {
		return nil
	}
	sess, err := s.sessions.GetByID(ctx, jti)
	if err != nil {
		return appDatabase(err)
	}
	if sess == nil || sess.UserID != userID || sess.TokenHash != hashToken(raw) {
		return nil
	}
	if err := s.sessions.RevokeByID(ctx, jti, at); err != nil {
		return appDatabase(err)
	}
	return nil
}

func (s *Service) issueResult(ctx context.Context, user *domain.User, clientKind string) (Result, error) {
	if user == nil {
		return Result{}, appUserNotFound()
	}
	clientKind = domain.NormalizeRefreshClient(clientKind)
	ttl := s.refreshTTLFor(clientKind)
	access, err := s.tokens.SignAccess(user.ID)
	if err != nil {
		return Result{}, domain.Internal("token_error", "could not issue access token", err)
	}
	refresh, jti, expiresAt, err := s.tokens.SignRefreshWithTTL(user.ID, ttl)
	if err != nil {
		return Result{}, domain.Internal("token_error", "could not issue refresh token", err)
	}

	if s.sessions != nil {
		sess := &domain.RefreshSession{
			ID:         jti,
			UserID:     user.ID,
			TokenHash:  hashToken(refresh),
			ExpiresAt:  expiresAt.UTC(),
			ClientKind: clientKind,
		}
		if err := s.sessions.Create(ctx, sess); err != nil {
			return Result{}, appDatabase(err)
		}
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

func (s *Service) refreshTTLFor(clientKind string) time.Duration {
	if domain.NormalizeRefreshClient(clientKind) == domain.RefreshClientAndroid {
		if s != nil && s.tokens != nil && s.tokens.AppRefreshTTL() > 0 {
			return s.tokens.AppRefreshTTL()
		}
		return 2160 * time.Hour
	}
	var ttl time.Duration
	if s != nil && s.tokens != nil {
		ttl = s.tokens.RefreshTTL()
	}
	if ttl <= 0 {
		ttl = 168 * time.Hour
	}
	return ttl
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
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
