package middleware

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/token"
	"github.com/dadiary/backend/pkg/response"
)

// UserLookup loads the account for an access token.
// A nil user means the account is gone. A non-nil error means the lookup failed.
type UserLookup func(ctx context.Context, id uuid.UUID) (*domain.User, error)

func firstLookup(lookups []UserLookup) UserLookup {
	if len(lookups) == 0 || lookups[0] == nil {
		return nil
	}
	return lookups[0]
}

// Context key for authenticated user ID in Fiber Locals.
const LocalsUserID = "auth_user_id"

// RequireAccessJWT parses a Bearer access token and stores the user UUID in c.Locals(LocalsUserID).
// When lookup is set, a token whose account no longer exists is rejected. Access JWTs
// live for hours, so account deletion has to fail closed here instead of waiting for expiry.
func RequireAccessJWT(svc *token.Service, lookup ...UserLookup) fiber.Handler {
	resolve := firstLookup(lookup)
	return func(c *fiber.Ctx) error {
		if svc == nil {
			return response.Error(c, fiber.StatusInternalServerError, "token_service_unavailable", "JWT service not configured")
		}
		raw := strings.TrimSpace(c.Get("Authorization"))
		if raw == "" || !strings.HasPrefix(strings.ToLower(raw), "bearer ") {
			return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing or invalid Authorization header")
		}
		tok := strings.TrimSpace(raw[7:])
		if tok == "" {
			return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "empty bearer token")
		}
		userID, err := svc.ParseAccessToken(tok)
		if err != nil {
			return response.Error(c, fiber.StatusUnauthorized, "invalid_token", "access token invalid or expired")
		}
		if resolve != nil {
			user, lookupErr := resolve(c.UserContext(), userID)
			if lookupErr != nil {
				return response.Error(c, fiber.StatusServiceUnavailable, "database_unavailable", "database is not available")
			}
			if user == nil {
				return response.Error(c, fiber.StatusUnauthorized, "invalid_token", "access token invalid or expired")
			}
		}
		c.Locals(LocalsUserID, userID)
		return c.Next()
	}
}

// OptionalAccessJWT attaches the user UUID when a valid Bearer token is present;
// anonymous requests continue without auth (for guest onboarding trial flows).
// A token for a deleted account is treated as anonymous so later analytics rows
// are not stamped with that user id. A lookup error keeps the token subject:
// narrow tests open a database that has no users table.
func OptionalAccessJWT(svc *token.Service, lookup ...UserLookup) fiber.Handler {
	resolve := firstLookup(lookup)
	return func(c *fiber.Ctx) error {
		if svc == nil {
			return c.Next()
		}
		raw := strings.TrimSpace(c.Get("Authorization"))
		if raw == "" || !strings.HasPrefix(strings.ToLower(raw), "bearer ") {
			return c.Next()
		}
		tok := strings.TrimSpace(raw[7:])
		if tok == "" {
			return c.Next()
		}
		userID, err := svc.ParseAccessToken(tok)
		if err != nil {
			return c.Next()
		}
		if resolve != nil {
			user, lookupErr := resolve(c.UserContext(), userID)
			if lookupErr != nil {
				c.Locals(LocalsUserID, userID)
				return c.Next()
			}
			if user == nil {
				return c.Next()
			}
		}
		c.Locals(LocalsUserID, userID)
		return c.Next()
	}
}

// UserIDFromLocals returns the UUID set by RequireAccessJWT, or uuid.Nil if absent/invalid.
func UserIDFromLocals(c *fiber.Ctx) uuid.UUID {
	v := c.Locals(LocalsUserID)
	id, ok := v.(uuid.UUID)
	if !ok || id == uuid.Nil {
		return uuid.Nil
	}
	return id
}
