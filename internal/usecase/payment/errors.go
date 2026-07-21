package payment

import "errors"

var (
	ErrUnavailable     = errors.New("payment service unavailable")
	ErrNotConfigured   = errors.New("sepay not configured")
	ErrInvalidRequest  = errors.New("invalid payment request")
	ErrUnauthorizedIPN = errors.New("unauthorized sepay webhook")
	ErrOrderNotFound   = errors.New("payment order not found")
	ErrAmountMismatch  = errors.New("payment amount mismatch")
	ErrInvalidUser     = errors.New("invalid user")
)
