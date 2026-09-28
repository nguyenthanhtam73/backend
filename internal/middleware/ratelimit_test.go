package middleware

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func newGuestLimitedApp(max int) *fiber.App {
	app := fiber.New(fiber.Config{
		ProxyHeader:        ClientIPHeader,
		EnableIPValidation: true,
	})
	app.Post("/ai", AILimiter(max, time.Minute), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

func postAs(t *testing.T, app *fiber.App, realIP string) int {
	t.Helper()
	req := httptest.NewRequest("POST", "/ai", nil)
	if realIP != "" {
		req.Header.Set(ClientIPHeader, realIP)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return resp.StatusCode
}

func TestAILimiter_GuestsBehindProxyGetSeparateBuckets(t *testing.T) {
	app := newGuestLimitedApp(1)

	if got := postAs(t, app, "203.0.113.10"); got != fiber.StatusOK {
		t.Fatalf("first guest first request: got %d, want 200", got)
	}
	if got := postAs(t, app, "203.0.113.10"); got != fiber.StatusTooManyRequests {
		t.Fatalf("first guest second request: got %d, want 429", got)
	}
	if got := postAs(t, app, "198.51.100.7"); got != fiber.StatusOK {
		t.Fatalf("second guest must not inherit the first guest's bucket: got %d, want 200", got)
	}
}

func TestAILimiter_MalformedClientIPFallsBackToPeer(t *testing.T) {
	app := newGuestLimitedApp(1)

	if got := postAs(t, app, "not-an-ip"); got != fiber.StatusOK {
		t.Fatalf("first request: got %d, want 200", got)
	}
	// Garbage and a missing header both resolve to the socket peer, so they
	// share a bucket instead of minting a fresh one per request.
	if got := postAs(t, app, "still-not-an-ip"); got != fiber.StatusTooManyRequests {
		t.Fatalf("second malformed request: got %d, want 429", got)
	}
	if got := postAs(t, app, ""); got != fiber.StatusTooManyRequests {
		t.Fatalf("missing header: got %d, want 429", got)
	}
}
