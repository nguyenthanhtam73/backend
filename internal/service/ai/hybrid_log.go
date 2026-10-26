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
//
// priorErr is the error from the provider that was attempted before this result
// (nil when the winner was tried first). primarySkippedNoKey is true when the
// configured primary had no API key, so the other provider ran as the only choice.
func logTextCoachSelection(pipeline string, res TextCoachResult, priorErr error, elapsed time.Duration, primarySkippedNoKey bool) {
	pipeline = strings.TrimSpace(pipeline)
	if pipeline == "" {
		pipeline = "text-coach"
	}
	elapsedMS := elapsed.Milliseconds()
	switch {
	case res.Fallback:
		failed := "openai"
		if res.Provider == TextCoachProviderOpenAI {
			failed = "claude"
		}
		reason := failed + "_error"
		if priorErr != nil && strings.Contains(priorErr.Error(), "deadline exceeded") {
			reason = failed + "_timeout"
		}
		msg := "Fallback to Claude for text coaching"
		if res.Provider == TextCoachProviderOpenAI {
			msg = "Fallback to GPT-4o for text coaching"
		}
		slog.Info(msg,
			"pipeline", pipeline,
			"model", res.Model,
			"reason", reason,
			"elapsed_ms", elapsedMS,
		)
	case res.Provider == TextCoachProviderClaude:
		args := []any{
			"pipeline", pipeline,
			"model", res.Model,
			"elapsed_ms", elapsedMS,
		}
		if primarySkippedNoKey {
			args = append(args, "reason", "no_openai_key")
		}
		slog.Info("Using Claude for text coaching", args...)
	case res.Provider == TextCoachProviderOpenAI:
		args := []any{
			"pipeline", pipeline,
			"model", res.Model,
			"elapsed_ms", elapsedMS,
		}
		if primarySkippedNoKey {
			args = append(args, "reason", "no_anthropic_key")
		}
		slog.Info("Using GPT-4o for text coaching", args...)
	}
}

// logVisionModelSelection logs which OpenAI vision model is used for photo analysis.
func logVisionModelSelection(pipeline, model string) {
	slog.Info("Using OpenAI for vision",
		"pipeline", pipeline,
		"model", model,
	)
}
