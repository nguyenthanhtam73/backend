package handler

import (
	"errors"

	"github.com/dadiary/backend/internal/domain"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	usageuc "github.com/dadiary/backend/internal/usecase/usage"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// mapPremiumGateError translates PremiumService / usage gate errors into HTTP 403
// with a stable code + reason so Free users never crash the client flow.
func mapPremiumGateError(c *fiber.Ctx, feature domain.Feature, err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, premiumuc.ErrQuotaExceeded), errors.Is(err, usageuc.ErrQuotaExceeded):
		return response.ErrorWithReason(
			c,
			fiber.StatusForbidden,
			"quota_exceeded",
			"monthly free limit reached — upgrade for unlimited access",
			premiumuc.ReasonQuotaExceeded,
			string(feature),
		)
	case errors.Is(err, premiumuc.ErrFeatureDenied), errors.Is(err, usageuc.ErrPremiumRequired):
		return response.ErrorWithReason(
			c,
			fiber.StatusForbidden,
			"premium_required",
			"upgrade your plan to use this feature",
			premiumuc.ReasonFeatureDenied,
			string(feature),
		)
	case errors.Is(err, premiumuc.ErrUnavailable), errors.Is(err, usageuc.ErrUnavailable):
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "premium gate unavailable")
	default:
		return response.Error(c, fiber.StatusInternalServerError, "internal_error", err.Error())
	}
}
