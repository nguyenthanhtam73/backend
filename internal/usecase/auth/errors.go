package auth

import (
	"errors"
	"net/http"

	"github.com/dadiary/backend/internal/domain"
)

// Sentinel errors — kept for errors.Is in handlers/tests.
// Prefer returning domain.AppError (via helpers below) from the usecase.
var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrEmailTaken         = errors.New("email already registered")
	ErrUsernameTaken      = errors.New("username already taken")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserInactive       = errors.New("user account is inactive")
	ErrUserNotFound       = errors.New("user not found")
	ErrDatabase           = errors.New("database error")
	ErrTokenConfig        = errors.New("token service misconfigured")
)

// AppError helpers — usecase returns these; Unwrap preserves sentinel for errors.Is.

func appInvalidInput(msg string) error {
	return domain.Wrap(ErrInvalidInput, http.StatusBadRequest, "invalid_input", msg)
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

func appDatabase(cause error) error {
	msg := "database is not available"
	if cause != nil {
		return domain.Wrap(ErrDatabase, http.StatusServiceUnavailable, "database_unavailable", msg)
	}
	return domain.Wrap(ErrDatabase, http.StatusServiceUnavailable, "database_unavailable", msg)
}

func appTokenConfig() error {
	return domain.Wrap(ErrTokenConfig, http.StatusInternalServerError, "misconfigured", "authentication misconfigured")
}
