package ai

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

const hybridLiveTestTimeout = 45 * time.Second

// TestTextCoachHybridLive verifies TextCoachCompletion against real APIs.
// Requires DADIARY_ANTHROPIC_API_KEY and/or DADIARY_OPENAI_API_KEY in .env.
func TestTextCoachHybridLive(t *testing.T) {
	cfg := loadCoachTestConfig(t)
	if !cfg.HasAnthropicKey() && !cfg.HasOpenAIKey() {
		t.Skip("set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), hybridLiveTestTimeout)
	defer cancel()

	client := &http.Client{Timeout: hybridLiveTestTimeout}

	system := "Reply JSON only."
	user := `{"provider_check":"ok"}`

	res, err := TextCoachCompletion(ctx, cfg, client, "hybrid-live-test", system, user)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "ok") {
		t.Fatalf("unexpected response: %q", res.Text)
	}
	t.Logf("provider=%s model=%s fallback=%v", res.Provider, res.Model, res.Fallback)
	if cfg.HasAnthropicKey() && res.Provider != TextCoachProviderClaude {
		t.Logf("NOTE: Claude key set but provider=%s (Claude may have failed and fell back)", res.Provider)
	}
}
