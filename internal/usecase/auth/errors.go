package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// Sentinel errors — kept for errors.Is in handlers/tests.
// Prefer returning domain.AppError (via helpers below) from the usecase.
var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrInvalidEmail       = errors.New("invalid email")
	ErrEmailTaken         = errors.New("email already registered")
	ErrUsernameTaken      = errors.New("username already taken")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserInactive       = errors.New("user account is inactive")
	ErrUserNotFound       = errors.New("user not found")
	ErrDatabase           = errors.New("database error")
	ErrTokenConfig        = errors.New("token service misconfigured")
	ErrInvalidRefresh     = errors.New("invalid or revoked refresh token")
)

// InvalidEmailCode is the stable API code for a malformed account email.
const InvalidEmailCode = "invalid_email"

// InvalidEmailMessage is the Vietnamese message shown when registration email
// format is rejected.
const InvalidEmailMessage = "Email chưa đúng, ví dụ: ten@gmail.com"

// StrangeInputMessage is the English register error for text that must not
// reach a query: whitespace or non-ASCII in an email, or invalid UTF-8, NUL,
// or other control characters in an email, username, or display name.
// pkg/usermsg.Plain maps it to the Vietnamese HTTP message.
const StrangeInputMessage = "email or name has an unusual character"

func appInvalidRefresh() error {
	return domain.Wrap(ErrInvalidRefresh, http.StatusUnauthorized, "invalid_refresh", ErrInvalidRefresh.Error())
}

// AppError helpers — usecase returns these; Unwrap preserves sentinel for errors.Is.

func appInvalidInput(msg string) error {
	return domain.Wrap(ErrInvalidInput, http.StatusBadRequest, "invalid_input", msg)
}

func appInvalidEmail() error {
	return domain.Wrap(ErrInvalidEmail, http.StatusBadRequest, InvalidEmailCode, InvalidEmailMessage)
}

func appEmailTaken() error {
	return domain.Wrap(ErrEmailTaken, http.StatusConflict, "email_taken", ErrEmailTaken.Error())
}

func appUsernameTaken() error {
	return domain.Wrap(ErrUsernameTaken, http.StatusConflict, "username_taken", ErrUsernameTaken.Error())
}

func appInvalidCredentials() error {
	return domain.Wrap(ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials", ErrInvalidCredentials.Error())
}

func appUserInactive() error {
	return domain.Wrap(ErrUserInactive, http.StatusForbidden, "user_inactive", ErrUserInactive.Error())
}

func appUserNotFound() error {
	return domain.Wrap(ErrUserNotFound, http.StatusNotFound, "user_not_found", ErrUserNotFound.Error())
}

// registerDBCause carries a register query failure into appDatabase so the
// driver error can be logged without leaving the package. email, username,
// and password are used only to redact the log line. They are never written
// as log fields.
type registerDBCause struct {
	op       string
	email    string
	username string
	password string
	cause    error
}

func (e *registerDBCause) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *registerDBCause) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func registerDatabase(op, email, username, password string, cause error) error {
	if cause == nil {
		return appDatabase(nil)
	}
	return appDatabase(&registerDBCause{
		op:       op,
		email:    email,
		username: username,
		password: password,
		cause:    cause,
	})
}

func appDatabase(cause error) error {
	logRegisterDatabase(cause)
	return domain.Wrap(ErrDatabase, http.StatusServiceUnavailable, "database_unavailable", "database is not available")
}

func logRegisterDatabase(cause error) {
	var rc *registerDBCause
	if !errors.As(cause, &rc) || rc == nil || rc.cause == nil {
		return
	}
	slog.Error("auth register database error",
		"op", rc.op,
		"err", redactRegisterSecrets(rc.cause.Error(), rc.email, rc.username, rc.password),
		"email_hash", emailHashPrefix(rc.email),
	)
}

// emailHashPrefix is the first 8 hex characters of SHA-256(normalized email).
func emailHashPrefix(normalizedEmail string) string {
	sum := sha256.Sum256([]byte(normalizedEmail))
	return hex.EncodeToString(sum[:])[:8]
}

func redactRegisterSecrets(msg, email, username, password string) string {
	// Email first so a username that is the local-part cannot split it.
	for _, secret := range []string{email, username, password} {
		if secret == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
	}
	return msg
}

func appTokenConfig() error {
	return domain.Wrap(ErrTokenConfig, http.StatusInternalServerError, "misconfigured", "authentication misconfigured")
}
