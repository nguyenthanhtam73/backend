package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/config"
)

func TestTextCoachProviderOrder(t *testing.T) {
	openaiThenClaude := [2]TextCoachProvider{TextCoachProviderOpenAI, TextCoachProviderClaude}
	claudeThenOpenAI := [2]TextCoachProvider{TextCoachProviderClaude, TextCoachProviderOpenAI}

	tests := []struct {
		name    string
		raw     string
		nilCfg  bool
		setting string
		want    [2]TextCoachProvider
	}{
		{name: "unset", nilCfg: true, setting: "unset", want: openaiThenClaude},
		{name: "empty", raw: "", setting: "unset", want: openaiThenClaude},
		{name: "whitespace", raw: "  \t", setting: "unset", want: openaiThenClaude},
		{name: "openai", raw: "openai", setting: "openai", want: openaiThenClaude},
		{name: "openai mixed case", raw: " OpenAI ", setting: "openai", want: openaiThenClaude},
		{name: "anthropic", raw: "anthropic", setting: "anthropic", want: claudeThenOpenAI},
		{name: "anthropic mixed case", raw: "ANTHROPIC", setting: "anthropic", want: claudeThenOpenAI},
		{name: "invalid", raw: "claude", setting: "invalid", want: openaiThenClaude},
		{name: "invalid model id", raw: "gpt-4o", setting: "invalid", want: openaiThenClaude},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cfg *config.Config
			if !tc.nilCfg {
				cfg = &config.Config{AI: config.AIConfig{PrimaryProvider: tc.raw}}
			}
			got := TextCoachProviderOrder(cfg)
			if len(got) != 2 || got[0] != tc.want[0] || got[1] != tc.want[1] {
				t.Fatalf("order = %v, want %v", got, tc.want[:])
			}
			setting, primary, fallback, _, _ := textCoachOrderSummary(cfg)
			if setting != tc.setting {
				t.Fatalf("setting = %q, want %q", setting, tc.setting)
			}
			if primary != string(tc.want[0]) || fallback != string(tc.want[1]) {
				t.Fatalf("summary primary=%s fallback=%s, want %s then %s", primary, fallback, tc.want[0], tc.want[1])
			}
		})
	}
}

func TestTextCoachOrderSummary_OmitsSecrets(t *testing.T) {
	const openAIKey = "sk-live-openai-secret"
	const claudeKey = "sk-ant-live-secret"
	cfg := &config.Config{
		OpenAI:    config.OpenAIConfig{APIKey: openAIKey, Model: "gpt-4o"},
		Anthropic: config.AnthropicConfig{APIKey: claudeKey, Model: "claude-sonnet-4-6"},
		AI:        config.AIConfig{PrimaryProvider: "openai"},
	}
	setting, primary, fallback, openAIModel, claudeModel := textCoachOrderSummary(cfg)
	blob := strings.Join([]string{setting, primary, fallback, openAIModel, claudeModel}, " ")
	if strings.Contains(blob, openAIKey) || strings.Contains(blob, claudeKey) || strings.Contains(blob, "sk-") {
		t.Fatalf("startup summary includes a secret: %s", blob)
	}
	if setting != "openai" || primary != "openai" || fallback != "claude" {
		t.Fatalf("summary = %s / %s / %s", setting, primary, fallback)
	}
	if openAIModel != "gpt-4o" || claudeModel != "claude-sonnet-4-6" {
		t.Fatalf("models = %s / %s", openAIModel, claudeModel)
	}
}
