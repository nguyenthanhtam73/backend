package auth

import "errors"

// Sentinel errors returned by Auth usecases. Handlers map these to HTTP status codes.
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
