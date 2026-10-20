package ai

import (
	"log/slog"
	"strings"

	"github.com/dadiary/backend/internal/config"
)

// Accepted AI_PRIMARY_PROVIDER values. Anything else, including empty, is openai.
const (
	aiPrimaryProviderOpenAI    = "openai"
	aiPrimaryProviderAnthropic = "anthropic"
)

// TextCoachProviderOrder is the only place that ranks text-coach providers.
// Skin check-in, onboarding skin, starter routine, daily feedback, routine
// suggest, and share-title cleanup all go through TextCoachCompletion, which
// consumes this order. No call site may rank providers itself.
//
//	openai / unset / unknown → GPT-4o, then Claude
//	anthropic                → Claude, then GPT-4o
//
// Matching is case-insensitive. "claude" is not an alias — it is unknown and
// therefore resolves to openai.
func TextCoachProviderOrder(cfg *config.Config) []TextCoachProvider {
	if aiPrimaryProviderSetting(cfg) == aiPrimaryProviderAnthropic {
		return []TextCoachProvider{TextCoachProviderClaude, TextCoachProviderOpenAI}
	}
	return []TextCoachProvider{TextCoachProviderOpenAI, TextCoachProviderClaude}
}

// aiPrimaryProviderSetting reports how AI_PRIMARY_PROVIDER was read:
// "unset", "openai", "anthropic", or "invalid". It never returns the raw
// value, so a mistaken secret in the variable cannot leak into logs.
func aiPrimaryProviderSetting(cfg *config.Config) string {
	raw := ""
	if cfg != nil {
		raw = cfg.AI.PrimaryProvider
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "unset"
	case aiPrimaryProviderOpenAI:
		return aiPrimaryProviderOpenAI
	case aiPrimaryProviderAnthropic:
		return aiPrimaryProviderAnthropic
	default:
		return "invalid"
	}
}

// LogTextCoachProviderOrder writes the resolved text-coach order once.
// Call it at process startup. The line names providers only — no API keys.
func LogTextCoachProviderOrder(cfg *config.Config) {
	setting, primary, fallback, openAIModel, claudeModel := textCoachOrderSummary(cfg)
	slog.Info("AI text provider order",
		"AI_PRIMARY_PROVIDER", setting,
		"primary", primary,
		"fallback", fallback,
		"openai_model", openAIModel,
		"claude_model", claudeModel,
	)
}

func textCoachOrderSummary(cfg *config.Config) (setting, primary, fallback, openAIModel, claudeModel string) {
	order := TextCoachProviderOrder(cfg)
	setting = aiPrimaryProviderSetting(cfg)
	primary = string(order[0])
	fallback = string(order[1])
	openAIModel = cfg.OpenAITextModel()
	claudeModel = cfg.AnthropicCoachModel()
	return setting, primary, fallback, openAIModel, claudeModel
}
