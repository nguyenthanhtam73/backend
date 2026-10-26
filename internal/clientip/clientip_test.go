package clientip

import (
	"net"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

func TestResolveRailwayDefaults(t *testing.T) {
	header, proxies, err := Resolve("", "")
	if err != nil {
		t.Fatal(err)
	}
	if header != DefaultHeader {
		t.Fatalf("header = %q", header)
	}
	if len(proxies) != 1 || proxies[0] != DefaultProxies {
		t.Fatalf("proxies = %#v", proxies)
	}
	if proxies[0] == "0.0.0.0/0" || proxies[0] == "::/0" {
		t.Fatal("default trusted proxies must be Railway's internal range")
	}
}

func TestResolveDisableAndOverride(t *testing.T) {
	header, proxies, err := Resolve("none", "100.64.0.0/10")
	if err != nil {
		t.Fatal(err)
	}
	if header != "" || proxies != nil {
		t.Fatalf("disabled header: header=%q proxies=%#v", header, proxies)
	}

	header, proxies, err = Resolve("X-Real-IP", "off")
	if err != nil {
		t.Fatal(err)
	}
	if header != "X-Real-IP" || len(proxies) != 0 {
		t.Fatalf("trust nobody: header=%q proxies=%#v", header, proxies)
	}

	header, proxies, err = Resolve("  X-Real-IP ", " 100.64.0.2 , 127.0.0.1 ")
	if err != nil {
		t.Fatal(err)
	}
	if header != "X-Real-IP" {
		t.Fatalf("header = %q", header)
	}
	if strings.Join(proxies, ",") != "100.64.0.2,127.0.0.1" {
		t.Fatalf("proxies = %#v", proxies)
	}
}

func TestResolveRejectsGarbage(t *testing.T) {
	if _, _, err := Resolve("X-Real-IP, X-Forwarded-For", ""); err == nil {
		t.Fatal("expected header error")
	}
	if _, _, err := Resolve("", "100.64.0.0/10, not-a-cidr"); err == nil {
		t.Fatal("expected proxy error")
	}
	if _, _, err := Resolve("", "10.0.0.1/99"); err == nil {
		t.Fatal("expected CIDR error")
	}
}

func TestUntrustedPeerIgnoresForgedHeaders(t *testing.T) {
	app := newIPApp(t)
	status, body := call(t, app, "203.0.113.9", map[string]string{
		"X-Real-IP":       "198.51.100.20",
		"X-Forwarded-For": "198.51.100.20, 203.0.113.9",
	})
	if status != fiber.StatusOK {
		t.Fatalf("status %d body %s", status, body)
	}
	if body != "203.0.113.9" {
		t.Fatalf("c.IP() = %q, forged headers must not replace the TCP peer", body)
	}
}

func TestTrustedProxyReturnsClientFromXRealIP(t *testing.T) {
	app := newIPApp(t)
	for _, peer := range []string{"100.64.0.2", "100.64.0.4", "100.127.255.254"} {
		status, body := call(t, app, peer, map[string]string{
			"X-Real-IP": "203.0.113.44",
		})
		if status != fiber.StatusOK || body != "203.0.113.44" {
			t.Fatalf("peer %s: status %d c.IP()=%q", peer, status, body)
		}
	}
}

func TestAddressJustOutsideRailwayRangeIsUntrusted(t *testing.T) {
	app := newIPApp(t)
	for _, peer := range []string{"100.63.255.255", "100.128.0.1", "8.8.8.8"} {
		_, body := call(t, app, peer, map[string]string{"X-Real-IP": "203.0.113.44"})
		if body != peer {
			t.Fatalf("peer %s: c.IP()=%q", peer, body)
		}
	}
}

func TestPrependedXFFIsIgnored(t *testing.T) {
	app := newIPApp(t)
	// Railway's proxy overwrites X-Real-IP. A fake the client stuck on the
	// front of X-Forwarded-For must not become the visitor address.
	_, body := call(t, app, "100.64.0.3", map[string]string{
		"X-Real-IP":       "203.0.113.50",
		"X-Forwarded-For": "192.0.2.8, 203.0.113.50",
	})
	if body != "203.0.113.50" {
		t.Fatalf("c.IP() = %q", body)
	}

	// With no X-Real-IP, do not parse X-Forwarded-For from either end.
	// The leftmost value is client-controlled if the proxy failed to strip it,
	// and the rightmost value on Railway is an internal hop.
	_, body = call(t, app, "100.64.0.3", map[string]string{
		"X-Forwarded-For": "192.0.2.8, 203.0.113.50",
	})
	if body != "100.64.0.3" {
		t.Fatalf("missing X-Real-IP: c.IP() = %q", body)
	}
}

func TestLocalDevUsesTCPPeer(t *testing.T) {
	app := newIPApp(t)
	_, body := call(t, app, "127.0.0.1", map[string]string{
		"X-Real-IP":       "203.0.113.7",
		"X-Forwarded-For": "203.0.113.7",
	})
	if body != "127.0.0.1" {
		t.Fatalf("local c.IP() = %q", body)
	}
}

func TestInvalidXRealIPFallsBackToPeer(t *testing.T) {
	app := newIPApp(t)
	_, body := call(t, app, "100.64.0.2", map[string]string{
		"X-Real-IP": "not-an-ip",
	})
	if body != "100.64.0.2" {
		t.Fatalf("c.IP() = %q", body)
	}
}

func newIPApp(t *testing.T) *fiber.App {
	t.Helper()
	app := fiberApp(t)
	app.Post("/guest", func(c *fiber.Ctx) error {
		return c.SendString(c.IP())
	})
	return app
}

func fiberApp(t *testing.T) *fiber.App {
	t.Helper()
	header, proxies, err := Resolve("", "")
	if err != nil {
		t.Fatal(err)
	}
	return fiber.New(Apply(fiber.Config{}, header, proxies))
}

func call(t *testing.T, app *fiber.App, peer string, headers map[string]string) (int, string) {
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
	return ctx.Response.StatusCode(), string(ctx.Response.Body())
}
