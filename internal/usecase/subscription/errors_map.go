package subscription

import "errors"

// MapError converts usecase errors to (HTTP status, code, message) for handlers.
// Codes are stable for FE i18n (subscription.* keys).
func MapError(err error) (status int, code, message string) {
	switch {
	case errors.Is(err, ErrUnavailable):
		return 503, "service_unavailable", "subscription service unavailable"
	case errors.Is(err, ErrInvalidUser):
		return 401, "unauthorized", "invalid user"
	case errors.Is(err, ErrNotEligible):
		return 409, "trial_not_eligible", "not eligible for trial"
	case errors.Is(err, ErrNotActive):
		return 409, "subscription_not_active", "no active subscription to cancel"
	case errors.Is(err, ErrAlreadyCanceled):
		return 409, "subscription_already_canceled", "subscription already canceled"
	case errors.Is(err, ErrInvalidPlan):
		return 400, "invalid_plan", "invalid plan tier"
	default:
		return 500, "subscription_failed", "subscription request failed"
	}
}
