package subscription

import "errors"

var (
	// ErrUnavailable means repos / db are not wired.
	ErrUnavailable = errors.New("subscription service unavailable")
	// ErrInvalidUser means the account does not exist.
	ErrInvalidUser = errors.New("invalid user")
	// ErrNotEligible means StartTrial was called but trial was already used / user is paid.
	ErrNotEligible = errors.New("not eligible for trial")
	// ErrNotActive means Cancel was called without an active paid/trial subscription.
	ErrNotActive = errors.New("no active subscription to cancel")
	// ErrAlreadyCanceled means Cancel was called twice.
	ErrAlreadyCanceled = errors.New("subscription already canceled")
	// ErrInvalidPlan means the requested plan_tier is not a paid tier.
	ErrInvalidPlan = errors.New("invalid plan tier")
)
