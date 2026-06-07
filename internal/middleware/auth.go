package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/dadiary/backend/internal/token"
	"github.com/dadiary/backend/pkg/response"
)

// Context key for authenticated user ID in Fiber Locals.
const LocalsUserID = "auth_user_id"

// RequireAccessJWT parses a Bearer access token and stores the user UUID in c.Locals(LocalsUserID).
func RequireAccessJWT(svc *token.Service) fiber.Handler {
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
		c.Locals(LocalsUserID, userID)
		return c.Next()
	}
}

// OptionalAccessJWT attaches the user UUID when a valid Bearer token is present;
// anonymous requests continue without auth (for guest onboarding trial flows).
func OptionalAccessJWT(svc *token.Service) fiber.Handler {
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
