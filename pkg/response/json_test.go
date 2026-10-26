package response

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestError_TranslatesMessageKeepsCodeAndStatus(t *testing.T) {
	app := fiber.New()
	app.Post("/login", func(c *fiber.Ctx) error {
		return Error(c, fiber.StatusUnauthorized, "invalid_credentials", "invalid email or password")
	})
	req := httptest.NewRequest(fiber.MethodPost, "/login", nil)
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status %d body %s", res.StatusCode, raw)
	}
	var env struct {
		Success bool `json:"success"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if env.Success || env.Error.Code != "invalid_credentials" {
		t.Fatalf("envelope: %+v", env)
	}
	if env.Error.Message != "Email hoặc mật khẩu chưa đúng." {
		t.Fatalf("message: %q", env.Error.Message)
	}
}
