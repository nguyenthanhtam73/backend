package ai

import (
	"context"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/platform/httpx"
	"github.com/dadiary/backend/pkg/retry"
)

// CallAIWithRetry wraps an AI operation with exponential backoff + jitter,
// retrying only transient failures (network errors, HTTP 429, HTTP 5xx).
// Permanent errors — auth (401/403), bad request (400), quota exhaustion and
// moderation rejections — fail fast without retrying. A cancelled/expired
// context stops retries immediately.
//
// `op` is a short label for logs/metrics (e.g. "openai-vision"). `fn` receives
// the (possibly deadline-bound) context so it honours cancellation per attempt.
//
// This is the canonical entry point every AI provider call in this package
// funnels through; the low-level HTTP round-trip lives inside `fn` (see
// httpx.PostJSON), so image reads and prompt building are NOT re-run on retry.
func CallAIWithRetry[T any](ctx context.Context, cfg *config.Config, op string, fn func(context.Context) (T, error)) (T, error) {
	return httpx.WithRetry(ctx, aiRetryConfig(cfg), op, fn)
}

// aiRetryConfig resolves the retry policy from app config, falling back to the
// package default when config is absent (e.g. in tests).
func aiRetryConfig(cfg *config.Config) retry.Config {
	if cfg == nil {
		return retry.Default()
	}
	return cfg.AI.Retry
}
