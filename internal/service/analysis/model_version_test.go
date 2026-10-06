package analysis

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/service/ai"
)

// Regression: hybrid pipeline model_version exceeded varchar(64) and SaveAnalysis failed
// after a successful coach run ("could not save completed analysis").
func TestHybridPipelineModelVersionFitsColumn(t *testing.T) {
	cfg := &config.Config{
		OpenAI:    config.OpenAIConfig{Model: "", VisionModel: ""},
		Anthropic: config.AnthropicConfig{Model: ""},
	}
	web := ai.PipelineModelVersion(cfg.OpenAIVisionModel(), "ok", cfg.AnthropicModel(), "anthropic", false, "")
	android := ai.PipelineModelVersion(cfg.OpenAIVisionModel(), "ok", cfg.AnthropicModel(), "anthropic", true, "android")
	const maxCol = 256
	for _, ver := range []string{web, android} {
		if len(ver) > maxCol {
			t.Fatalf("model_version len=%d exceeds column size %d: %q", len(ver), maxCol, ver)
		}
		if len(strings.Split(ver, "|")) != 3 {
			t.Fatalf("model_version must stay three |-segments: %q", ver)
		}
	}
	if len(web) <= 64 {
		t.Fatalf("expected default hybrid model_version to exceed legacy varchar(64), got len=%d", len(web))
	}
	if strings.Contains(web, "+android") {
		t.Fatalf("web model_version changed: %q", web)
	}
	webFallback := ai.PipelineModelVersion(cfg.OpenAIVisionModel(), "ok", cfg.AnthropicModel(), "anthropic", true, "web")
	if android != webFallback+"+android" {
		t.Fatalf("android %q want %q", android, webFallback+"+android")
	}
}
