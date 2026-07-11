package ai

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Sentinel errors: one that counts toward tripping (transient), one that does
// not (permanent / caller error).
var (
	errTrip   = errors.New("transient provider failure")
	errNoTrip = errors.New("permanent caller failure")
)

// tripOnErrTrip mimics httpx.IsRetryable: only errTrip counts as a provider
// health failure that should move the breaker toward Open.
func tripOnErrTrip(err error) bool { return errors.Is(err, errTrip) }

// fakeClock is a manually-advanced clock for deterministic time-based tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// testConfig builds a small, fast breaker config wired to the fake clock.
func testConfig(clock *fakeClock) CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 3,
		ResetTimeout:     30 * time.Second,
		SuccessThreshold: 2,
		HalfOpenMaxCalls: 1,
		HalfOpenTimeout:  10 * time.Second,
		ShouldTrip:       tripOnErrTrip,
		Now:              clock.now,
	}
}

// run is a tiny helper that drives one guarded call through the breaker.
func run(cb *CircuitBreaker, err error) error {
	if e := cb.beforeCall(); e != nil {
		return e
	}
	cb.afterCall(err)
	return err
}

// -----------------------------------------------------------------------------
// Defaults / config
// -----------------------------------------------------------------------------

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	d := DefaultCircuitBreakerConfig()
	if d.FailureThreshold != 5 {
		t.Errorf("FailureThreshold = %d, want 5", d.FailureThreshold)
	}
	if d.ResetTimeout != 30*time.Second {
		t.Errorf("ResetTimeout = %v, want 30s", d.ResetTimeout)
	}
	if d.SuccessThreshold != 2 {
		t.Errorf("SuccessThreshold = %d, want 2", d.SuccessThreshold)
	}
}

func TestWithDefaults_FillsZeroFields(t *testing.T) {
	got := CircuitBreakerConfig{}.withDefaults()
	if got.FailureThreshold != 5 || got.ResetTimeout != 30*time.Second || got.SuccessThreshold != 2 {
		t.Fatalf("zero config not defaulted: %+v", got)
	}
	if got.HalfOpenMaxCalls != got.SuccessThreshold {
		t.Errorf("HalfOpenMaxCalls = %d, want SuccessThreshold %d", got.HalfOpenMaxCalls, got.SuccessThreshold)
	}
	if got.ShouldTrip == nil || got.Now == nil {
		t.Error("ShouldTrip/Now should be defaulted to non-nil")
	}
}

// -----------------------------------------------------------------------------
// Closed-state behaviour
// -----------------------------------------------------------------------------

// Consecutive counting failures up to the threshold trip the breaker Open.
func TestBreaker_TripsOpenAfterThreshold(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cb := NewCircuitBreaker("test", testConfig(clock))

	for i := 0; i < 2; i++ {
		if err := run(cb, errTrip); !errors.Is(err, errTrip) {
			t.Fatalf("attempt %d: err = %v, want errTrip", i, err)
		}
		if cb.State() != StateClosed {
			t.Fatalf("attempt %d: state = %v, want closed (below threshold)", i, cb.State())
		}
	}
	// Third consecutive counting failure hits FailureThreshold=3 => Open.
	_ = run(cb, errTrip)
	if cb.State() != StateOpen {
		t.Fatalf("state = %v, want open after %d failures", cb.State(), 3)
	}
}

// A success resets the consecutive-failure counter so the breaker stays Closed.
func TestBreaker_SuccessResetsFailureCount(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cb := NewCircuitBreaker("test", testConfig(clock))

	_ = run(cb, errTrip)
	_ = run(cb, errTrip)
	_ = run(cb, nil) // reset
	_ = run(cb, errTrip)
	_ = run(cb, errTrip)
	if cb.State() != StateClosed {
		t.Fatalf("state = %v, want closed (success reset the counter)", cb.State())
	}
}

// Non-counting errors (permanent/caller failures) must NOT trip the breaker.
func TestBreaker_NonCountingErrorsDoNotTrip(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cb := NewCircuitBreaker("test", testConfig(clock))

	for i := 0; i < 10; i++ {
		_ = run(cb, errNoTrip)
	}
	if cb.State() != StateClosed {
		t.Fatalf("state = %v, want closed (non-counting errors ignored)", cb.State())
	}
}

// -----------------------------------------------------------------------------
// Open-state behaviour
// -----------------------------------------------------------------------------

// While Open, calls are rejected fast with ErrCircuitOpen and fn never runs.
func TestBreaker_OpenRejectsFast(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cb := NewCircuitBreaker("test", testConfig(clock))
	tripToOpen(t, cb)

	if err := cb.beforeCall(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("beforeCall = %v, want ErrCircuitOpen", err)
	}
}

// After ResetTimeout elapses, the next call is allowed as a Half-Open probe.
func TestBreaker_OpenToHalfOpenAfterTimeout(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cb := NewCircuitBreaker("test", testConfig(clock))
	tripToOpen(t, cb)

	clock.advance(31 * time.Second) // past ResetTimeout (30s)
	if err := cb.beforeCall(); err != nil {
		t.Fatalf("beforeCall after cool-down = %v, want nil (probe allowed)", err)
	}
	if cb.State() != StateHalfOpen {
		t.Fatalf("state = %v, want half-open", cb.State())
	}
}

// -----------------------------------------------------------------------------
// Half-Open behaviour
// -----------------------------------------------------------------------------

// Enough consecutive probe successes close the circuit again.
func TestBreaker_HalfOpenClosesAfterSuccesses(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cb := NewCircuitBreaker("test", testConfig(clock))
	tripToOpen(t, cb)
	clock.advance(31 * time.Second)

	// SuccessThreshold = 2 successful probes.
	for i := 0; i < 2; i++ {
		if err := cb.beforeCall(); err != nil {
			t.Fatalf("probe %d: beforeCall = %v, want nil", i, err)
		}
		cb.afterCall(nil)
	}
	if cb.State() != StateClosed {
		t.Fatalf("state = %v, want closed after successful probes", cb.State())
	}
}

// A counting failure during Half-Open immediately re-opens the breaker.
func TestBreaker_HalfOpenReopensOnFailure(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cb := NewCircuitBreaker("test", testConfig(clock))
	tripToOpen(t, cb)
	clock.advance(31 * time.Second)

	if err := cb.beforeCall(); err != nil {
		t.Fatalf("probe beforeCall = %v, want nil", err)
	}
	cb.afterCall(errTrip) // probe fails
	if cb.State() != StateOpen {
		t.Fatalf("state = %v, want open (failed probe re-opens)", cb.State())
	}
}

// Half-Open caps concurrent probes at HalfOpenMaxCalls (1 here): a second
// concurrent probe is rejected while the first is still in flight.
func TestBreaker_HalfOpenLimitsConcurrentProbes(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cb := NewCircuitBreaker("test", testConfig(clock))
	tripToOpen(t, cb)
	clock.advance(31 * time.Second)

	if err := cb.beforeCall(); err != nil { // first probe reserves the only slot
		t.Fatalf("first probe = %v, want nil", err)
	}
	if err := cb.beforeCall(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("second concurrent probe = %v, want ErrCircuitOpen", err)
	}
}

// If Half-Open lingers past HalfOpenTimeout without enough successes, the next
// evaluation reverts to Open.
func TestBreaker_HalfOpenTimeoutReopens(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cb := NewCircuitBreaker("test", testConfig(clock))
	tripToOpen(t, cb)
	clock.advance(31 * time.Second)

	if err := cb.beforeCall(); err != nil { // enter half-open
		t.Fatalf("probe = %v, want nil", err)
	}
	cb.afterCall(nil) // one success (not enough; threshold is 2)

	clock.advance(11 * time.Second) // exceed HalfOpenTimeout (10s)
	if err := cb.beforeCall(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("beforeCall after half-open timeout = %v, want ErrCircuitOpen", err)
	}
	if cb.State() != StateOpen {
		t.Fatalf("state = %v, want open after half-open timeout", cb.State())
	}
}

// -----------------------------------------------------------------------------
// Generic wrapper: CallAIWithCircuitBreaker
// -----------------------------------------------------------------------------

func TestCallAIWithCircuitBreaker_OpenFailsFastWithoutRunningFn(t *testing.T) {
	resetBreakersForTest()
	SetDefaultCircuitBreakerConfig(CircuitBreakerConfig{
		FailureThreshold: 2,
		ShouldTrip:       tripOnErrTrip,
	})
	t.Cleanup(func() {
		resetBreakersForTest()
		SetDefaultCircuitBreakerConfig(CircuitBreakerConfig{})
	})

	ctx := context.Background()
	fail := func(context.Context) (int, error) { return 0, errTrip }

	// Two counting failures trip the breaker (FailureThreshold=2).
	_, _ = CallAIWithCircuitBreaker(ctx, "op", fail)
	_, _ = CallAIWithCircuitBreaker(ctx, "op", fail)

	// Now the breaker is Open: fn must NOT run and we get ErrCircuitOpen.
	ran := false
	_, err := CallAIWithCircuitBreaker(ctx, "op", func(context.Context) (int, error) {
		ran = true
		return 1, nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("err = %v, want ErrCircuitOpen", err)
	}
	if ran {
		t.Fatal("fn ran while circuit was open; expected fast rejection")
	}
}

func TestCallAIWithCircuitBreaker_SuccessReturnsValue(t *testing.T) {
	resetBreakersForTest()
	t.Cleanup(resetBreakersForTest)

	got, err := CallAIWithCircuitBreaker(context.Background(), "op", func(context.Context) (string, error) {
		return "ok", nil
	})
	if err != nil || got != "ok" {
		t.Fatalf("got %q err %v, want ok/nil", got, err)
	}
}

func TestCallAIWithCircuitBreaker_PreCancelledContext(t *testing.T) {
	resetBreakersForTest()
	t.Cleanup(resetBreakersForTest)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ran := false
	_, err := CallAIWithCircuitBreaker(ctx, "op", func(context.Context) (int, error) {
		ran = true
		return 0, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if ran {
		t.Fatal("fn ran despite cancelled context")
	}
}

// Independent op labels must have independent breakers.
func TestCallAIWithCircuitBreaker_IsolatedPerOp(t *testing.T) {
	resetBreakersForTest()
	SetDefaultCircuitBreakerConfig(CircuitBreakerConfig{FailureThreshold: 1, ShouldTrip: tripOnErrTrip})
	t.Cleanup(func() {
		resetBreakersForTest()
		SetDefaultCircuitBreakerConfig(CircuitBreakerConfig{})
	})

	ctx := context.Background()
	_, _ = CallAIWithCircuitBreaker(ctx, "claude", func(context.Context) (int, error) { return 0, errTrip })

	// "claude" is now open, but "openai" must still be usable.
	if _, err := CallAIWithCircuitBreaker(ctx, "claude", func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("claude err = %v, want ErrCircuitOpen", err)
	}
	if got, err := CallAIWithCircuitBreaker(ctx, "openai", func(context.Context) (int, error) { return 1, nil }); err != nil || got != 1 {
		t.Fatalf("openai got %d err %v, want 1/nil (independent breaker)", got, err)
	}
}

// tripToOpen drives FailureThreshold counting failures to force Open.
func tripToOpen(t *testing.T, cb *CircuitBreaker) {
	t.Helper()
	for i := 0; i < cb.cfg.FailureThreshold; i++ {
		_ = run(cb, errTrip)
	}
	if cb.State() != StateOpen {
		t.Fatalf("setup: breaker not open after %d failures (state %v)", cb.cfg.FailureThreshold, cb.State())
	}
}

// resetBreakersForTest clears the lazily-created breaker registry so wrapper
// tests don't leak state into each other.
func resetBreakersForTest() {
	breakerMu.Lock()
	defer breakerMu.Unlock()
	breakers = map[string]*CircuitBreaker{}
}
