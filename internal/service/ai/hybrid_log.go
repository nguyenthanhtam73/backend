package ai

import (
	"log/slog"
	"strings"
)

// logTextCoachSelection emits a clear Info line when a text-coaching provider is chosen.
func logTextCoachSelection(pipeline string, res TextCoachResult, claudeErr error) {
	pipeline = strings.TrimSpace(pipeline)
	if pipeline == "" {
		pipeline = "text-coach"
	}
	switch {
	case res.Fallback:
		reason := "claude_error"
		if claudeErr != nil && strings.Contains(claudeErr.Error(), "deadline exceeded") {
			reason = "claude_timeout"
		}
		slog.Info("Fallback to GPT-4o for text coaching",
			"pipeline", pipeline,
			"model", res.Model,
			"reason", reason,
		)
	case res.Provider == TextCoachProviderClaude:
		slog.Info("Using Claude for text coaching",
			"pipeline", pipeline,
			"model", res.Model,
		)
	case res.Provider == TextCoachProviderOpenAI:
		slog.Info("Using GPT-4o for text coaching",
			"pipeline", pipeline,
			"model", res.Model,
			"reason", "no_anthropic_key",
		)
	}
}

// logVisionModelSelection logs which OpenAI vision model is used for photo analysis.
func logVisionModelSelection(pipeline, model string) {
	slog.Info("Using OpenAI for vision",
		"pipeline", pipeline,
		"model", model,
	)
}
