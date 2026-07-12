package ai

import (
	"log/slog"
	"strings"
	"time"
)

// logTextCoachSelection emits a clear Info line when a text-coaching provider is
// chosen. elapsed is the wall time spent in the winning provider call and is logged
// as elapsed_ms so we can watch the coach latency after switching the default model
// (e.g. Sonnet → Haiku) without needing a separate profiler.
func logTextCoachSelection(pipeline string, res TextCoachResult, claudeErr error, elapsed time.Duration) {
	pipeline = strings.TrimSpace(pipeline)
	if pipeline == "" {
		pipeline = "text-coach"
	}
	elapsedMS := elapsed.Milliseconds()
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
			"elapsed_ms", elapsedMS,
		)
	case res.Provider == TextCoachProviderClaude:
		slog.Info("Using Claude for text coaching",
			"pipeline", pipeline,
			"model", res.Model,
			"elapsed_ms", elapsedMS,
		)
	case res.Provider == TextCoachProviderOpenAI:
		slog.Info("Using GPT-4o for text coaching",
			"pipeline", pipeline,
			"model", res.Model,
			"reason", "no_anthropic_key",
			"elapsed_ms", elapsedMS,
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
