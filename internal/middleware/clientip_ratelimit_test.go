package middleware

import (
	"net"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/clientip"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/valyala/fasthttp"
)

func TestGuestLimiterBucketsRealClientsSeparately(t *testing.T) {
	app := newGuestLimiterApp(t)
	peer := "100.64.0.2"

	if status := postGuest(t, app, peer, map[string]string{"X-Real-IP": "203.0.113.10"}); status != fiber.StatusOK {
		t.Fatalf("client A first status %d", status)
	}
	// Same proxy address, different visitor. A site-wide proxy bucket would 429 here.
	if status := postGuest(t, app, peer, map[string]string{"X-Real-IP": "203.0.113.11"}); status != fiber.StatusOK {
		t.Fatalf("client B first status %d", status)
	}
	if status := postGuest(t, app, peer, map[string]string{"X-Real-IP": "203.0.113.10"}); status != fiber.StatusTooManyRequests {
		t.Fatalf("client A second status %d", status)
	}
	// A fake XFF in front of the same real client stays in that client's bucket.
	status := postGuest(t, app, peer, map[string]string{
		"X-Real-IP":       "203.0.113.11",
		"X-Forwarded-For": "192.0.2.99, 203.0.113.11",
	})
	if status != fiber.StatusTooManyRequests {
		t.Fatalf("client B with prepended XFF status %d", status)
	}
}

func TestGuestLimiterForgedHeadersDoNotSplitBuckets(t *testing.T) {
	app := newGuestLimiterApp(t)
	peer := "198.51.100.8"
	first := postGuest(t, app, peer, map[string]string{
		"X-Real-IP":       "203.0.113.1",
		"X-Forwarded-For": "203.0.113.1",
	})
	second := postGuest(t, app, peer, map[string]string{
		"X-Real-IP":       "203.0.113.2",
		"X-Forwarded-For": "192.0.2.1, 203.0.113.2",
	})
	if first != fiber.StatusOK || second != fiber.StatusTooManyRequests {
		t.Fatalf("untrusted peer statuses %d then %d", first, second)
	}
}

func TestAuthenticatedLimiterStillKeysByUser(t *testing.T) {
	app := newTrustedProxyApp(t)
	user := uuid.New()
	app.Post("/guest", func(c *fiber.Ctx) error {
		c.Locals(LocalsUserID, user)
		return c.Next()
	}, AILimiter(1, time.Minute), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	a := postGuest(t, app, "100.64.0.2", map[string]string{"X-Real-IP": "203.0.113.10"})
	b := postGuest(t, app, "100.64.0.2", map[string]string{"X-Real-IP": "203.0.113.11"})
	if a != fiber.StatusOK || b != fiber.StatusTooManyRequests {
		t.Fatalf("same user from two client IPs: %d then %d", a, b)
	}
}

func newGuestLimiterApp(t *testing.T) *fiber.App {
	t.Helper()
	app := newTrustedProxyApp(t)
	// Max 1 makes the bucket boundary visible. Production caps are unchanged.
	app.Post("/guest", AILimiter(1, time.Minute), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

func newTrustedProxyApp(t *testing.T) *fiber.App {
	t.Helper()
	header, proxies, err := clientip.Resolve("", "")
	if err != nil {
		t.Fatal(err)
	}
	return fiber.New(clientip.Apply(fiber.Config{}, header, proxies))
}

func postGuest(t *testing.T, app *fiber.App, peer string, headers map[string]string) int {
	t.Helper()
	ip := net.ParseIP(peer)
	if ip == nil {
		t.Fatalf("peer %q", peer)
	}
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod(fiber.MethodPost)
	ctx.Request.SetRequestURI("/guest")
	for k, v := range headers {
		ctx.Request.Header.Set(k, v)
	}
	ctx.SetRemoteAddr(&net.TCPAddr{IP: ip, Port: 12345})
	app.Handler()(ctx)
	return ctx.Response.StatusCode()
}
