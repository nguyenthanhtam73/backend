package config

import "testing"

func TestHybridConfig_ModelDefaults(t *testing.T) {
	cfg := &Config{
		OpenAI:    OpenAIConfig{Model: "", VisionModel: ""},
		Anthropic: AnthropicConfig{Model: ""},
	}
	if got := cfg.AnthropicModel(); got != "claude-sonnet-4-6" {
		t.Fatalf("AnthropicModel=%q", got)
	}
	if got := cfg.OpenAITextModel(); got != "gpt-4o" {
		t.Fatalf("OpenAITextModel=%q", got)
	}
	if got := cfg.OpenAIVisionModel(); got != "gpt-4o" {
		t.Fatalf("OpenAIVisionModel=%q", got)
	}
}

func TestHybridConfig_VisionModelOverride(t *testing.T) {
	cfg := &Config{
		OpenAI: OpenAIConfig{Model: "gpt-4o", VisionModel: "gpt-4o-mini"},
	}
	if got := cfg.OpenAIVisionModel(); got != "gpt-4o-mini" {
		t.Fatalf("OpenAIVisionModel=%q want gpt-4o-mini", got)
	}
}

func TestHybridConfig_HasKeys(t *testing.T) {
	withBoth := &Config{
		OpenAI:    OpenAIConfig{APIKey: "sk-test"},
		Anthropic: AnthropicConfig{APIKey: "sk-ant"},
	}
	if !withBoth.HasOpenAIKey() || !withBoth.HasAnthropicKey() {
		t.Fatal("expected both keys")
	}
	empty := &Config{}
	if empty.HasOpenAIKey() || empty.HasAnthropicKey() {
		t.Fatal("expected no keys")
	}
}
