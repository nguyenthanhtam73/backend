package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/google/uuid"

	"github.com/dadiary/backend/pkg/response"
)

// AILimiter returns a per-user / per-IP rate limiter for expensive AI routes.
//
// Why per-route limiting instead of a single global limiter:
//   - Different AI endpoints have very different cost profiles (multipart
//     skin photo analysis is several seconds + an OpenAI vision call;
//     routine suggest is a single short Anthropic call).
//   - A single global cap would either be too generous (allowing abuse on
//     the cheap path) or punish heavy users on the more expensive path.
//
// `max` is the number of requests allowed inside `expiration`. The key is
// derived from the authenticated user when `RequireAccessJWT` has populated
// locals; otherwise we fall back to the client IP so unauthenticated bursts
// still get capped (Fiber's built-in `c.IP()` honours the
// `X-Forwarded-For` header when running behind a trusted proxy).
//
// On overflow we return our standard JSON error envelope so the frontend's
// `getApiErrorMessage` helper renders a friendly banner instead of an
// unstyled fiber default.
func AILimiter(max int, expiration time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: expiration,
		KeyGenerator: func(c *fiber.Ctx) string {
			if id, ok := c.Locals(LocalsUserID).(uuid.UUID); ok && id != uuid.Nil {
				return "u:" + id.String()
			}
			return "ip:" + c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			// 429 with a plain-text retry hint inside our envelope; the
			// frontend already maps `error.message` into its inline banners.
			return response.Error(
				c,
				fiber.StatusTooManyRequests,
				"rate_limited",
				"Too many requests. Please slow down for a minute and try again.",
			)
		},
		// Don't count failed requests against the user — gives them room to
		// retry after a transient backend hiccup without being locked out.
		SkipFailedRequests: true,
	})
}
