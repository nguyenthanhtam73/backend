package middleware

import (
	"strings"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// RequireAdmin ensures the JWT user is listed in DADIARY_ADMIN_EMAILS.
//
// Must run after RequireAccessJWT so LocalsUserID is populated.
func RequireAdmin(cfg *config.Config, users *repository.GormUserRepository) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if cfg == nil || users == nil {
			return response.Error(c, fiber.StatusInternalServerError, "misconfigured", "admin auth not configured")
		}
		uid := UserIDFromLocals(c)
		if uid == uuid.Nil {
			return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
		}
		user, err := users.GetByID(c.UserContext(), uid)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "admin_error", "could not verify admin")
		}
		if user == nil {
			return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "user not found")
		}
		if !cfg.IsAdminEmail(user.Email) {
			return response.Error(c, fiber.StatusForbidden, "forbidden", "admin access required")
		}
		c.Locals("auth_user_email", strings.TrimSpace(user.Email))
		return c.Next()
	}
}
