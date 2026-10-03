package middleware

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func TestAccessLogRedactsRegisterErrorEmail(t *testing.T) {
	const email = "thao.nguyen+ads@gmail.com"
	var buf bytes.Buffer
	app := fiber.New()
	app.Use(logger.New(accessLoggerConfig(&buf)))
	app.Post("/auth/register", func(c *fiber.Ctx) error {
		return fmt.Errorf("duplicate Key (email)=(%s)", email)
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"email":"`+email+`","password":"password1"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()

	got := buf.String()
	if strings.Contains(got, email) || strings.Contains(got, "thao.nguyen") || strings.Contains(got, "password1") {
		t.Fatalf("access log leaked request data:\n%s", got)
	}
	if !strings.Contains(got, "t***@gmail.com") {
		t.Fatalf("access log missing masked email:\n%s", got)
	}
	if !strings.Contains(got, "POST /auth/register") {
		t.Fatalf("access log lost the route:\n%s", got)
	}
}

func TestAccessLogDropsBodyHeadersAndFullIP(t *testing.T) {
	var buf bytes.Buffer
	cfg := accessLoggerConfig(&buf)
	cfg.Format = "${ip} ${ips} ${body} ${reqHeaders} ${error}\n"
	app := fiber.New()
	app.Use(logger.New(cfg))
	app.Post("/skin-checks", func(c *fiber.Ctx) error {
		return fmt.Errorf("note leaked")
	})

	req := httptest.NewRequest(http.MethodPost, "/skin-checks", strings.NewReader(`{"user_note":"da đỏ","email":"lan@gmail.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer super-secret-token")
	req.Header.Set("X-Forwarded-For", "203.0.113.44")
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()

	got := buf.String()
	for _, leak := range []string{"da đỏ", "lan@gmail.com", "super-secret-token", "203.0.113.44", "Bearer"} {
		if strings.Contains(got, leak) {
			t.Fatalf("access log leaked %q:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "***") {
		t.Fatalf("expected redacted placeholders:\n%s", got)
	}
}
