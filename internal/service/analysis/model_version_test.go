package analysis

import (
	"fmt"
	"testing"

	"github.com/dadiary/backend/internal/config"
)

// Regression: hybrid pipeline model_version exceeded varchar(64) and SaveAnalysis failed
// after a successful coach run ("could not save completed analysis").
func TestHybridPipelineModelVersionFitsColumn(t *testing.T) {
	cfg := &config.Config{
		OpenAI:    config.OpenAIConfig{Model: "", VisionModel: ""},
		Anthropic: config.AnthropicConfig{Model: ""},
	}
	ver := fmt.Sprintf(
		"pipeline=hybrid|vision=%s(%s)|coach=%s(%s)",
		cfg.OpenAIVisionModel(), "ok",
		cfg.AnthropicModel(), "anthropic",
	)
	const maxCol = 256
	if len(ver) > maxCol {
		t.Fatalf("model_version len=%d exceeds column size %d: %q", len(ver), maxCol, ver)
	}
	if len(ver) <= 64 {
		t.Fatalf("expected default hybrid model_version to exceed legacy varchar(64), got len=%d", len(ver))
	}
}
