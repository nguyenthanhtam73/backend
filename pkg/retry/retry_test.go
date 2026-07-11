package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Shared sentinel errors used across cases so tests can assert with errors.Is.
var (
	errTransient = errors.New("transient failure")
	errPermanent = errors.New("permanent failure")
)

// fastConfig keeps delays tiny so timing-sensitive tests stay quick and stable.
func fastConfig() Config {
	return Config{
		MaxRetries:        3,
		InitialDelay:      time.Millisecond,
		MaxDelay:          5 * time.Millisecond,
		BackoffMultiplier: 2,
	}
}

// Classifiers used to drive different branches.
func retryAll(error) bool  { return true }
func retryNone(error) bool { return false }

// -----------------------------------------------------------------------------
// DoValue: core success / retry / exhaustion / classifier branches
// -----------------------------------------------------------------------------

// Success on the very first attempt: fn is called exactly once and no retry
// happens even though the classifier would allow it.
func TestDoValue_SucceedsFirstTry(t *testing.T) {
	var calls int
	got, err := DoValue(context.Background(), fastConfig(), "op", retryAll, func(context.Context) (int, error) {
		calls++
		return 42, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Fatalf("got %d, want 42", got)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

// Transient failures on the first attempts, then success: the returned value
// must come from the successful attempt and the call count must match.
func TestDoValue_RetriesThenSucceeds(t *testing.T) {
	var calls int
	got, err := DoValue(context.Background(), fastConfig(), "op", retryAll, func(context.Context) (string, error) {
		calls++
		if calls < 3 {
			return "", errTransient
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("got %q, want %q", got, "ok")
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

// Every attempt fails with a retryable error: the engine exhausts its budget
// (MaxRetries+1 total attempts), returns the last error, and the zero value.
func TestDoValue_ExhaustsBudget(t *testing.T) {
	var calls int
	got, err := DoValue(context.Background(), fastConfig(), "op", retryAll, func(context.Context) (int, error) {
		calls++
		return 7, errTransient
	})
	if !errors.Is(err, errTransient) {
		t.Fatalf("err = %v, want errTransient", err)
	}
	// MaxRetries=3 => 1 initial attempt + 3 retries = 4 total.
	if calls != 4 {
		t.Fatalf("calls = %d, want 4", calls)
	}
	// On failure the caller must receive the zero value, never a partial one.
	if got != 0 {
		t.Fatalf("got %d, want zero value 0 on failure", got)
	}
}

// A non-retryable error must fail fast: fn is invoked exactly once regardless
// of the retry budget.
func TestDoValue_NonRetryableFailsFast(t *testing.T) {
	var calls int
	_, err := DoValue(context.Background(), fastConfig(), "op", retryNone, func(context.Context) (int, error) {
		calls++
		return 0, errPermanent
	})
	if !errors.Is(err, errPermanent) {
		t.Fatalf("err = %v, want errPermanent", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retries for non-retryable error)", calls)
	}
}

// A nil classifier means "never retry": behaves like a non-retryable error.
func TestDoValue_NilClassifierNeverRetries(t *testing.T) {
	var calls int
	_, err := DoValue(context.Background(), fastConfig(), "op", nil, func(context.Context) (int, error) {
		calls++
		return 0, errTransient
	})
	if !errors.Is(err, errTransient) {
		t.Fatalf("err = %v, want errTransient", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (nil classifier => never retry)", calls)
	}
}

// A selective classifier (the pattern httpx.IsRetryable follows): retry only a
// specific error. Retryable errors are retried; the first permanent error stops
// the loop immediately even though budget remains.
func TestDoValue_SelectiveClassifier(t *testing.T) {
	isRetryable := func(err error) bool { return errors.Is(err, errTransient) }

	var calls int
	_, err := DoValue(context.Background(), fastConfig(), "op", isRetryable, func(context.Context) (int, error) {
		calls++
		if calls <= 2 {
			return 0, errTransient // retried
		}
		return 0, errPermanent // stops here
	})
	if !errors.Is(err, errPermanent) {
		t.Fatalf("err = %v, want errPermanent", err)
	}
	// 2 transient (retried) + 1 permanent (fail fast) = 3 calls total.
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

// The zero-valued Config must transparently pick up the package defaults
// (so callers can pass Config{} safely). Success path incurs no delay.
func TestDoValue_ZeroConfigUsesDefaults(t *testing.T) {
	var calls int
	got, err := DoValue(context.Background(), Config{}, "op", retryAll, func(context.Context) (bool, error) {
		calls++
		return true, nil
	})
	if err != nil || !got || calls != 1 {
		t.Fatalf("got=%v err=%v calls=%d, want true/nil/1", got, err, calls)
	}
}

// -----------------------------------------------------------------------------
// Do: the no-value wrapper delegates to DoValue with the same semantics
// -----------------------------------------------------------------------------

func TestDo_SucceedsFirstTry(t *testing.T) {
	var calls int
	err := Do(context.Background(), fastConfig(), "op", retryAll, func(context.Context) error {
		calls++
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d, want nil/1", err, calls)
	}
}

func TestDo_RetriesThenSucceeds(t *testing.T) {
	var calls int
	err := Do(context.Background(), fastConfig(), "op", retryAll, func(context.Context) error {
		calls++
		if calls < 2 {
			return errTransient
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d, want nil/2", err, calls)
	}
}

func TestDo_ExhaustsBudget(t *testing.T) {
	var calls int
	err := Do(context.Background(), fastConfig(), "op", retryAll, func(context.Context) error {
		calls++
		return errTransient
	})
	if !errors.Is(err, errTransient) {
		t.Fatalf("err = %v, want errTransient", err)
	}
	if calls != 4 {
		t.Fatalf("calls = %d, want 4", calls)
	}
}

// -----------------------------------------------------------------------------
// Context handling: pre-cancelled, cancelled mid-retry, deadline exceeded
// -----------------------------------------------------------------------------

// An already-cancelled context short-circuits before fn ever runs.
func TestDoValue_PreCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel up front

	var calls int
	_, err := DoValue(ctx, fastConfig(), "op", retryAll, func(context.Context) (int, error) {
		calls++
		return 0, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0 (fn must not run when ctx already cancelled)", calls)
	}
}

// Cancelling during the backoff wait stops retries immediately. The returned
// error joins the last transient error with context.Canceled so both are
// observable via errors.Is.
func TestDoValue_CancelDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Use a longer initial delay so the cancel lands while we're waiting.
	cfg := Config{MaxRetries: 5, InitialDelay: 50 * time.Millisecond, MaxDelay: time.Second, BackoffMultiplier: 2}

	var calls int32
	// Cancel shortly after the first failure, i.e. during the backoff sleep.
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := DoValue(ctx, cfg, "op", retryAll, func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		return 0, errTransient
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want to wrap context.Canceled", err)
	}
	// The underlying transient error must be preserved alongside cancellation.
	if !errors.Is(err, errTransient) {
		t.Fatalf("err = %v, want to also wrap errTransient", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want 1 (cancel during backoff stops before next attempt)", got)
	}
}

// A context deadline that expires while retries are in flight terminates the
// loop and surfaces context.DeadlineExceeded.
func TestDoValue_DeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// Backoff base (>= 25ms) is long enough that the 30ms deadline fires during
	// a wait, so the loop ends via ctx rather than exhausting the budget.
	cfg := Config{MaxRetries: 10, InitialDelay: 50 * time.Millisecond, MaxDelay: time.Second, BackoffMultiplier: 2}

	var calls int32
	_, err := DoValue(ctx, cfg, "op", retryAll, func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		return 0, errTransient
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	// It must give up well before the 10-retry budget is used.
	if got := atomic.LoadInt32(&calls); got >= 10 {
		t.Fatalf("calls = %d, want the deadline to cut retries short", got)
	}
}

// -----------------------------------------------------------------------------
// Backoff + jitter
// -----------------------------------------------------------------------------

// Each computed delay must fall within the "equal jitter" window [base/2, base]
// where base grows exponentially per attempt (until capped). This verifies both
// exponential growth and the presence of jitter.
func TestBackoffDelay_WithinJitterWindowAndGrows(t *testing.T) {
	cfg := Config{
		InitialDelay:      100 * time.Millisecond,
		MaxDelay:          time.Hour, // effectively uncapped for these attempts
		BackoffMultiplier: 2,
	}

	var prevBase float64
	for attempt := 0; attempt < 5; attempt++ {
		base := float64(cfg.InitialDelay) * pow(cfg.BackoffMultiplier, attempt)

		// The theoretical base must double each attempt (mult=2).
		if attempt > 0 && base <= prevBase {
			t.Fatalf("attempt %d: base %.0f did not grow beyond previous %.0f", attempt, base, prevBase)
		}
		prevBase = base

		// Sample many times; every sample must be inside [base/2, base].
		for i := 0; i < 200; i++ {
			d := float64(backoffDelay(cfg, attempt))
			if d < base/2 || d > base {
				t.Fatalf("attempt %d sample %d: delay %.0f outside [%.0f, %.0f]", attempt, i, d, base/2, base)
			}
		}
	}
}

// Jitter must actually randomise the delay: repeated samples for a fixed
// attempt should not all be identical.
func TestBackoffDelay_JitterVaries(t *testing.T) {
	cfg := Config{InitialDelay: time.Second, MaxDelay: time.Hour, BackoffMultiplier: 2}
	first := backoffDelay(cfg, 3)
	varied := false
	for i := 0; i < 50; i++ {
		if backoffDelay(cfg, 3) != first {
			varied = true
			break
		}
	}
	if !varied {
		t.Fatal("backoffDelay produced identical values across samples; jitter not applied")
	}
}

// Beyond a certain attempt the exponential curve must be clamped to MaxDelay,
// so no sampled delay can ever exceed it.
func TestBackoffDelay_CapsAtMaxDelay(t *testing.T) {
	cfg := Config{InitialDelay: time.Second, MaxDelay: 2 * time.Second, BackoffMultiplier: 10}
	for attempt := 0; attempt < 6; attempt++ {
		for i := 0; i < 50; i++ {
			if d := backoffDelay(cfg, attempt); d > cfg.MaxDelay {
				t.Fatalf("attempt %d: delay %v exceeds MaxDelay %v", attempt, d, cfg.MaxDelay)
			}
		}
	}
}

// Retries genuinely wait: with a measurable InitialDelay, two forced retries
// must consume at least the summed minimum (jittered) backoff.
func TestDoValue_ActuallyDelaysBetweenRetries(t *testing.T) {
	cfg := Config{MaxRetries: 3, InitialDelay: 20 * time.Millisecond, MaxDelay: time.Second, BackoffMultiplier: 2}

	var calls int
	start := time.Now()
	_, err := DoValue(context.Background(), cfg, "op", retryAll, func(context.Context) (int, error) {
		calls++
		if calls < 3 { // fail twice, then succeed
			return 0, errTransient
		}
		return 1, nil
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Minimum backoff = base0/2 + base1/2 = 10ms + 20ms = 30ms.
	minExpected := 30 * time.Millisecond
	if elapsed < minExpected {
		t.Fatalf("elapsed %v < expected minimum backoff %v", elapsed, minExpected)
	}
}

// -----------------------------------------------------------------------------
// Config helpers
// -----------------------------------------------------------------------------

// Default returns the documented policy.
func TestDefaultConfig(t *testing.T) {
	d := Default()
	if d.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", d.MaxRetries)
	}
	if d.InitialDelay != 500*time.Millisecond {
		t.Errorf("InitialDelay = %v, want 500ms", d.InitialDelay)
	}
	if d.MaxDelay != 5*time.Second {
		t.Errorf("MaxDelay = %v, want 5s", d.MaxDelay)
	}
	if d.BackoffMultiplier != 2 {
		t.Errorf("BackoffMultiplier = %v, want 2", d.BackoffMultiplier)
	}
}

// withDefaults fills only the zero/invalid fields and leaves valid ones intact.
func TestWithDefaults(t *testing.T) {
	// Fully empty => equals Default.
	if got := (Config{}).withDefaults(); got != Default() {
		t.Errorf("empty config withDefaults() = %+v, want %+v", got, Default())
	}

	// A BackoffMultiplier of 1 (no growth) is treated as invalid and reset.
	if got := (Config{BackoffMultiplier: 1}).withDefaults(); got.BackoffMultiplier != 2 {
		t.Errorf("BackoffMultiplier reset = %v, want 2", got.BackoffMultiplier)
	}

	// Valid custom values are preserved.
	custom := Config{MaxRetries: 7, InitialDelay: time.Second, MaxDelay: 10 * time.Second, BackoffMultiplier: 3}
	if got := custom.withDefaults(); got != custom {
		t.Errorf("withDefaults mutated valid config: got %+v, want %+v", got, custom)
	}
}

// pow is a tiny integer-exponent helper for the backoff assertions (mirrors the
// math.Pow the implementation uses, kept local to avoid an import just for tests).
func pow(base float64, exp int) float64 {
	out := 1.0
	for i := 0; i < exp; i++ {
		out *= base
	}
	return out
}
