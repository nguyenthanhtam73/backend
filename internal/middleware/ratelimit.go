package middleware

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/google/uuid"

	"github.com/dadiary/backend/internal/domain"
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
		// Count every attempt — including 4xx/5xx after the handler runs.
		// AI routes often call the vendor before returning an error (timeout,
		// moderation, parse failure). Skipping failures would let an attacker
		// burn OpenAI/Anthropic credit indefinitely without hitting 429.
		SkipFailedRequests: false,
	})
}

func funnelLimitReached(c *fiber.Ctx) error {
	return response.Error(
		c,
		fiber.StatusTooManyRequests,
		"rate_limited",
		"Too many requests. Please slow down for a minute and try again.",
	)
}

func newFunnelLimiter(max int, window time.Duration, key func(*fiber.Ctx) string) fiber.Handler {
	if max <= 0 {
		max = 60
	}
	if window < time.Second {
		window = time.Minute
	}
	return limiter.New(limiter.Config{
		Max:                max,
		Expiration:         window,
		KeyGenerator:       key,
		LimitReached:       funnelLimitReached,
		SkipFailedRequests: false,
	})
}

// FunnelEventIPLimiter caps funnel ingest per client IP.
// The IP is used only as an in-memory bucket key and is not persisted.
func FunnelEventIPLimiter(max int, window time.Duration) fiber.Handler {
	return newFunnelLimiter(max, window, func(c *fiber.Ctx) string {
		return "ip:" + c.IP()
	})
}

// FunnelEventSessionLimiter caps funnel ingest per client session_id.
// Requests without a usable session_id share the IP bucket.
func FunnelEventSessionLimiter(max int, window time.Duration) fiber.Handler {
	return newFunnelLimiter(max, window, func(c *fiber.Ctx) string {
		if sid := funnelSessionID(c); sid != "" {
			return "sid:" + sid
		}
		return "ip:" + c.IP()
	})
}

// funnelSessionID reads session_id for the rate-limit key.
// Oversized bodies are not parsed; those requests fall back to the IP bucket.
func funnelSessionID(c *fiber.Ctx) string {
	body := c.Body()
	if len(body) == 0 || len(body) > domain.MaxFunnelBodyBytes {
		return ""
	}
	var probe struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	sid := strings.TrimSpace(probe.SessionID)
	if sid == "" || utf8.RuneCountInString(sid) > domain.MaxFunnelSessionIDRunes {
		return ""
	}
	return sid
}
