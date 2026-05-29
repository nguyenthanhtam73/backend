package ai

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestTextCoachHybridLive verifies TextCoachCompletion against real APIs.
// Requires DADIARY_ANTHROPIC_API_KEY and/or DADIARY_OPENAI_API_KEY in .env.
func TestTextCoachHybridLive(t *testing.T) {
	cfg := loadCoachTestConfig(t)
	if !cfg.HasAnthropicKey() && !cfg.HasOpenAIKey() {
		t.Skip("set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	system := "You are a test assistant. Reply with exactly: {\"provider_check\":\"ok\"}"
	user := "Return the JSON now."

	res, err := TextCoachCompletion(ctx, cfg, nil, "hybrid-live-test", system, user)
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
