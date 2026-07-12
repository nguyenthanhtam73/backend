// Package retry provides a small, dependency-free retry engine with
// exponential backoff and jitter. It is provider-agnostic: the caller supplies
// an `IsRetryable` classifier so the same engine can wrap AI calls, database
// calls, or any other transient-failure-prone operation.
//
// Design goals:
//   - Exponential backoff + jitter to avoid the "thundering herd" problem when
//     many callers retry a rate-limited/overloaded upstream at the same time.
//   - Respect context cancellation: a cancelled/expired context stops retries
//     immediately (no more sleeping, no more attempts).
//   - Observable: every retry, success-after-retry, and exhaustion is logged
//     via slog (structured) and counted via expvar (a zero-dependency, stdlib
//     metrics surface). See metrics.go for the counter definitions.
//   - Extensible: the classifier is a plain func, so richer policies (or a
//     future circuit breaker wrapping `DoValue`) can be layered on top.
package retry

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"math/rand"
	"time"
)

// Config controls backoff behaviour. Zero-valued fields fall back to the
// package defaults via withDefaults, so a partially-filled Config is safe.
type Config struct {
	// MaxRetries is the number of retries AFTER the first attempt.
	// MaxRetries=3 means up to 4 total attempts.
	MaxRetries int `mapstructure:"max_retries"`
	// InitialDelay is the base delay before the first retry.
	InitialDelay time.Duration `mapstructure:"initial_delay"`
	// MaxDelay caps the computed backoff so it never grows unbounded.
	MaxDelay time.Duration `mapstructure:"max_delay"`
	// BackoffMultiplier grows the delay each attempt (delay *= multiplier).
	BackoffMultiplier float64 `mapstructure:"backoff_multiplier"`
}

// Default returns the recommended retry policy for AI calls.
func Default() Config {
	return Config{
		MaxRetries:        3,
		InitialDelay:      500 * time.Millisecond,
		MaxDelay:          5 * time.Second,
		BackoffMultiplier: 2,
	}
}

// withDefaults fills any zero-valued field from Default so callers can pass a
// partial config (or the zero value) without surprising behaviour.
func (c Config) withDefaults() Config {
	d := Default()
	if c.MaxRetries <= 0 {
		c.MaxRetries = d.MaxRetries
	}
	if c.InitialDelay <= 0 {
		c.InitialDelay = d.InitialDelay
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = d.MaxDelay
	}
	if c.BackoffMultiplier <= 1 {
		c.BackoffMultiplier = d.BackoffMultiplier
	}
	return c
}

// Do runs fn with retries. See DoValue for details; Do is the no-return-value
// convenience wrapper.
func Do(ctx context.Context, cfg Config, op string, isRetryable func(error) bool, fn func(context.Context) error) error {
	_, err := DoValue(ctx, cfg, op, isRetryable, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return err
}

// DoValue runs fn until it succeeds, the error is deemed non-retryable, the
// retry budget is exhausted, or ctx is cancelled — whichever comes first.
//
//   - op is a short label used in logs/metrics (e.g. "openai-chat"). It is
//     optional; an empty op is recorded under "unknown" in the per-op metric.
//   - isRetryable decides whether a given error warrants another attempt. A nil
//     classifier means "never retry" (fn runs exactly once).
//   - fn receives ctx so it can honour cancellation/deadlines per attempt.
func DoValue[T any](
	ctx context.Context,
	cfg Config,
	op string,
	isRetryable func(error) bool,
	fn func(context.Context) (T, error),
) (T, error) {
	cfg = cfg.withDefaults()
	if isRetryable == nil {
		isRetryable = func(error) bool { return false }
	}

	var zero T
	// attempt is 0-based; attempt > 0 means we have already retried at least once.
	for attempt := 0; ; attempt++ {
		// Stop before doing more work if the caller already cancelled.
		if err := ctx.Err(); err != nil {
			return zero, err
		}

		val, err := fn(ctx)
		if err == nil {
			// Only flag "recovered" successes — a clean first attempt is the
			// normal case and shouldn't inflate the success-after-retry metric.
			if attempt > 0 {
				recordSuccessAfterRetry()
				slog.Info("retry: succeeded after retrying",
					"op", op,
					"retries", attempt,
					"attempts", attempt+1,
				)
			}
			return val, nil
		}

		// Permanent errors (auth, bad request, moderation, quota) fail fast.
		if !isRetryable(err) {
			return zero, err
		}

		// Out of budget: surface the last error as the definitive failure.
		if attempt >= cfg.MaxRetries {
			recordExhausted(err)
			slog.Warn("retry: exhausted budget",
				"op", op,
				"attempts", attempt+1,
				"max_attempts", cfg.MaxRetries+1,
				"last_error", err.Error(),
			)
			return zero, err
		}

		delay := backoffDelay(cfg, attempt)
		recordRetry(op, err)
		slog.Warn("retry: transient failure, backing off",
			"op", op,
			"attempt", attempt+1,
			"max_attempts", cfg.MaxRetries+1,
			"delay", delay.String(),
			"error", err.Error(),
		)

		// Sleep for `delay`, but wake up immediately if ctx is cancelled so a
		// cancelled request never blocks on a pending backoff.
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}

// backoffDelay computes the delay before the retry at 0-based `attempt` using
// exponential growth capped at MaxDelay, then applies "equal jitter":
// half the delay is fixed and half is randomised. This preserves a sensible
// minimum wait while spreading retries out to avoid synchronised bursts.
func backoffDelay(cfg Config, attempt int) time.Duration {
	d := float64(cfg.InitialDelay) * math.Pow(cfg.BackoffMultiplier, float64(attempt))
	if max := float64(cfg.MaxDelay); d > max {
		d = max
	}
	half := d / 2
	jittered := half + rand.Float64()*half
	return time.Duration(jittered)
}
