package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/gofiber/fiber/v2"
)

func guidanceTestApp(t *testing.T) *fiber.App {
	t.Helper()
	app := fiber.New()
	h := NewOnboardingAnalyzeHandler(&config.Config{})
	app.Post("/api/v1/onboarding/product-guidance", h.ProductGuidance)
	return app
}

func postGuidance(t *testing.T, app *fiber.App, body string) (*http.Response, dto.OnboardingProductGuidanceResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/product-guidance", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Success bool                                  `json:"success"`
		Data    dto.OnboardingProductGuidanceResponse `json:"data"`
	}
	if res.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode %q: %v", string(raw), err)
		}
		if !env.Success {
			t.Fatalf("envelope not success: %s", string(raw))
		}
	}
	return res, env.Data
}

// Guests finish onboarding without logging in, so the endpoint must answer
// unauthenticated requests with real affiliate CTAs.
func TestProductGuidanceEndpointReturnsCTAsForGuest(t *testing.T) {
	app := guidanceTestApp(t)
	res, data := postGuidance(t, app, `{
		"locale": "vi",
		"skin_type": "normal",
		"goal": "glow",
		"concerns": ["hyperpigmentation", "uneven_texture"]
	}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", res.StatusCode)
	}
	if len(data.ProductGuidance) == 0 {
		t.Fatal("expected product guidance cards")
	}
	if len(data.ProductSuggestions) == 0 {
		t.Fatal("expected at least one affiliate suggestion")
	}
	for _, g := range data.ProductGuidance {
		if strings.TrimSpace(g.Why) == "" {
			t.Fatalf("step %q has no why", g.Step)
		}
	}
	for _, p := range data.ProductSuggestions {
		if !strings.Contains(p.AffiliateLink, "shopee") {
			t.Fatalf("unexpected link host: %q", p.AffiliateLink)
		}
	}
}

func TestProductGuidanceEndpointDefaultsToVietnamese(t *testing.T) {
	app := guidanceTestApp(t)
	_, data := postGuidance(t, app, `{"goal":"clear_acne"}`)
	if len(data.ProductGuidance) == 0 {
		t.Fatal("expected guidance for an empty-ish payload")
	}
	joined := ""
	for _, g := range data.ProductGuidance {
		joined += g.NameOrCategory + " " + g.Why + " "
	}
	if !strings.Contains(joined, "Kem chống nắng") {
		t.Fatalf("expected Vietnamese copy by default, got: %s", joined)
	}
}

func TestProductGuidanceEndpointRejectsBadBody(t *testing.T) {
	app := guidanceTestApp(t)
	res, _ := postGuidance(t, app, `not json`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", res.StatusCode)
	}
}
