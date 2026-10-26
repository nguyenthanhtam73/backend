package middleware

import (
	"encoding/json"
	"fmt"
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

// AccountDeleteLimiter caps DELETE /me password attempts.
// The message names the real window in English and Vietnamese. AILimiter's
// shared copy says "a minute", which is wrong for this 15-minute bucket.
func AccountDeleteLimiter(max int, window time.Duration) fiber.Handler {
	if max <= 0 {
		max = 5
	}
	if window < time.Second {
		window = 15 * time.Minute
	}
	mins := int(window / time.Minute)
	if mins < 1 {
		mins = 1
	}
	message := fmt.Sprintf(
		"Too many attempts. Please wait %d minutes and try again. Bạn đã thử quá nhiều lần. Vui lòng đợi %d phút rồi thử lại.",
		mins, mins,
	)
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: window,
		KeyGenerator: func(c *fiber.Ctx) string {
			if id, ok := c.Locals(LocalsUserID).(uuid.UUID); ok && id != uuid.Nil {
				return "acctdel:" + id.String()
			}
			return "acctdel-ip:" + c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return response.Error(c, fiber.StatusTooManyRequests, "rate_limited", message)
		},
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

// FunnelEventGlobalLimiter is a process-wide safety cap for funnel ingest.
// Mount it after FunnelEventSessionLimiter so requests that limiter rejects
// are never counted here. It does not use c.IP(). Behind Railway that value
// is the proxy, so a per-IP bucket would be one bucket for every visitor.
func FunnelEventGlobalLimiter(max int, window time.Duration) fiber.Handler {
	return newFunnelLimiter(max, window, func(*fiber.Ctx) string {
		return "global"
	})
}

// FunnelEventSessionLimiter caps funnel ingest per client session_id.
// A missing or invalid session_id is rejected here and does not call next,
// so a following global limiter is not charged. Oversized or non-JSON bodies
// share one bucket. This does not call c.IP().
func FunnelEventSessionLimiter(max int, window time.Duration) fiber.Handler {
	inner := newFunnelLimiter(max, window, func(c *fiber.Ctx) string {
		if sid := funnelSessionID(c); sid != "" {
			return "sid:" + sid
		}
		return "nosession"
	})
	return func(c *fiber.Ctx) error {
		if _, msg, readable := classifyFunnelSession(c.Body()); readable && msg != "" {
			return response.Error(c, fiber.StatusBadRequest, "invalid_funnel_event", msg)
		}
		return inner(c)
	}
}

// funnelSessionID reads session_id for the rate-limit key.
// Oversized, non-JSON, and invalid session ids are not used as keys.
func funnelSessionID(c *fiber.Ctx) string {
	sid, _, _ := classifyFunnelSession(c.Body())
	return sid
}

// classifyFunnelSession parses session_id for limiting.
// readable is false for an empty, oversized, or non-JSON body; those requests
// are left to the handler. When readable is true and msg is set, session_id
// is missing or invalid and the request must be rejected before later limiters.
func classifyFunnelSession(body []byte) (sid, msg string, readable bool) {
	if len(body) == 0 || len(body) > domain.MaxFunnelBodyBytes {
		return "", "", false
	}
	var probe struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return "", "", false
	}
	sid = strings.TrimSpace(probe.SessionID)
	switch {
	case sid == "":
		return "", "session_id is required", true
	case utf8.RuneCountInString(sid) > domain.MaxFunnelSessionIDRunes:
		return "", "session_id is too long", true
	case !funnelSessionPrintable(sid):
		return "", "session_id is invalid", true
	default:
		return sid, "", true
	}
}

func funnelSessionPrintable(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
