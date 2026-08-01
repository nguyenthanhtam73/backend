package middleware

import (
	"strings"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// RequireAdmin ensures the JWT user is listed in DADIARY_ADMIN_EMAILS (full admin).
//
// Must run after RequireAccessJWT so LocalsUserID is populated.
func RequireAdmin(cfg *config.Config, users *repository.GormUserRepository) fiber.Handler {
	return requireEmailAccess(cfg, users, func(cfg *config.Config, email string) bool {
		return cfg.IsAdminEmail(email)
	}, "admin access required")
}

// RequireSkinReview ensures the JWT user may use /admin/skin-review* —
// either full admin (DADIARY_ADMIN_EMAILS) or skin-review operator
// (DADIARY_SKIN_REVIEW_EMAILS). Does not grant other /admin/* routes.
//
// Must run after RequireAccessJWT so LocalsUserID is populated.
func RequireSkinReview(cfg *config.Config, users *repository.GormUserRepository) fiber.Handler {
	return requireEmailAccess(cfg, users, func(cfg *config.Config, email string) bool {
		return cfg.CanSkinReviewEmail(email)
	}, "skin review access required")
}

func requireEmailAccess(
	cfg *config.Config,
	users *repository.GormUserRepository,
	allow func(*config.Config, string) bool,
	forbidMsg string,
) fiber.Handler {
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
		if !allow(cfg, user.Email) {
			return response.Error(c, fiber.StatusForbidden, "forbidden", forbidMsg)
		}
		c.Locals("auth_user_email", strings.TrimSpace(user.Email))
		return c.Next()
	}
}
