package config

import (
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/pkg/retry"
)

// validRetry is a baseline, known-good retry config the negative cases mutate
// one field at a time so each test isolates a single rule.
func validRetry() retry.Config {
	return retry.Config{
		MaxRetries:        3,
		InitialDelay:      500 * time.Millisecond,
		MaxDelay:          5 * time.Second,
		BackoffMultiplier: 2,
	}
}

// A fully valid config must pass with no error.
func TestValidateRetryConfig_Valid(t *testing.T) {
	if err := validateRetryConfig(validRetry()); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

// max_retries must be > 0.
func TestValidateRetryConfig_MaxRetriesZero(t *testing.T) {
	cfg := validRetry()
	cfg.MaxRetries = 0
	err := validateRetryConfig(cfg)
	if err == nil {
		t.Fatal("expected error for max_retries = 0")
	}
	if !strings.Contains(err.Error(), "ai.retry.max_retries must be greater than 0") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// initial_delay must be > 0.
func TestValidateRetryConfig_InitialDelayZero(t *testing.T) {
	cfg := validRetry()
	cfg.InitialDelay = 0
	err := validateRetryConfig(cfg)
	if err == nil {
		t.Fatal("expected error for initial_delay = 0")
	}
	if !strings.Contains(err.Error(), "ai.retry.initial_delay must be greater than 0") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// max_delay must be >= initial_delay.
func TestValidateRetryConfig_MaxDelayLessThanInitial(t *testing.T) {
	cfg := validRetry()
	cfg.InitialDelay = 2 * time.Second
	cfg.MaxDelay = 1 * time.Second // below the floor
	err := validateRetryConfig(cfg)
	if err == nil {
		t.Fatal("expected error for max_delay < initial_delay")
	}
	if !strings.Contains(err.Error(), "ai.retry.max_delay must be greater than or equal to initial_delay") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// max_delay == initial_delay is the boundary and must be accepted.
func TestValidateRetryConfig_MaxDelayEqualsInitial(t *testing.T) {
	cfg := validRetry()
	cfg.InitialDelay = 2 * time.Second
	cfg.MaxDelay = 2 * time.Second
	if err := validateRetryConfig(cfg); err != nil {
		t.Fatalf("max_delay == initial_delay should be valid: %v", err)
	}
}

// backoff_multiplier must be >= 1.
func TestValidateRetryConfig_BackoffMultiplierLessThanOne(t *testing.T) {
	cfg := validRetry()
	cfg.BackoffMultiplier = 0.5
	err := validateRetryConfig(cfg)
	if err == nil {
		t.Fatal("expected error for backoff_multiplier < 1")
	}
	if !strings.Contains(err.Error(), "ai.retry.backoff_multiplier must be greater than or equal to 1") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// backoff_multiplier == 1 is the boundary (constant delay) and must be accepted.
func TestValidateRetryConfig_BackoffMultiplierOne(t *testing.T) {
	cfg := validRetry()
	cfg.BackoffMultiplier = 1
	if err := validateRetryConfig(cfg); err != nil {
		t.Fatalf("backoff_multiplier == 1 should be valid: %v", err)
	}
}

// When several rules are broken at once, all messages must be reported together
// (errors.Join), so operators can fix everything in one pass.
func TestValidateRetryConfig_MultipleErrorsJoined(t *testing.T) {
	cfg := retry.Config{
		MaxRetries:        0,   // invalid
		InitialDelay:      0,   // invalid
		MaxDelay:          0,   // ok relative to initial (0 >= 0)
		BackoffMultiplier: 0.5, // invalid
	}
	err := validateRetryConfig(cfg)
	if err == nil {
		t.Fatal("expected joined validation errors")
	}
	msg := err.Error()
	for _, want := range []string{
		"ai.retry.max_retries must be greater than 0",
		"ai.retry.initial_delay must be greater than 0",
		"ai.retry.backoff_multiplier must be greater than or equal to 1",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("joined error missing %q; got: %v", want, msg)
		}
	}
}

// -----------------------------------------------------------------------------
// End-to-end via Load, driven purely by env vars so the test never depends on a
// real config.yaml on disk (Load runs in env-only mode inside the package dir).
// -----------------------------------------------------------------------------

// With no retry env set, Load applies defaults and the result validates cleanly.
func TestLoad_RetryDefaults(t *testing.T) {
	cfg, err := Load(".env")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	r := cfg.AI.Retry
	if r.MaxRetries != 3 || r.InitialDelay != 500*time.Millisecond || r.MaxDelay != 5*time.Second || r.BackoffMultiplier != 2 {
		t.Fatalf("unexpected default retry config: %+v", r)
	}
}

// Env overrides are parsed and pass validation.
func TestLoad_RetryFromEnvValid(t *testing.T) {
	t.Setenv("DADIARY_AI_RETRY_MAX_RETRIES", "5")
	t.Setenv("DADIARY_AI_RETRY_INITIAL_DELAY", "250ms")
	t.Setenv("DADIARY_AI_RETRY_MAX_DELAY", "10s")
	t.Setenv("DADIARY_AI_RETRY_BACKOFF_MULTIPLIER", "3")

	cfg, err := Load(".env")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	r := cfg.AI.Retry
	if r.MaxRetries != 5 || r.InitialDelay != 250*time.Millisecond || r.MaxDelay != 10*time.Second || r.BackoffMultiplier != 3 {
		t.Fatalf("env overrides not applied: %+v", r)
	}
}

// An incoherent env combination (max_delay < initial_delay) must fail Load.
func TestLoad_RetryFromEnvInvalid(t *testing.T) {
	t.Setenv("DADIARY_AI_RETRY_MAX_RETRIES", "3")
	t.Setenv("DADIARY_AI_RETRY_INITIAL_DELAY", "10s")
	t.Setenv("DADIARY_AI_RETRY_MAX_DELAY", "1s") // below initial_delay
	t.Setenv("DADIARY_AI_RETRY_BACKOFF_MULTIPLIER", "2")

	_, err := Load(".env")
	if err == nil {
		t.Fatal("expected Load to fail on invalid retry config")
	}
	if !strings.Contains(err.Error(), "ai.retry.max_delay must be greater than or equal to initial_delay") {
		t.Fatalf("unexpected error: %v", err)
	}
}
