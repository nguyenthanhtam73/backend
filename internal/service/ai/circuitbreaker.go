package ai

// This file adds a per-operation circuit breaker in front of outbound AI
// provider calls (Claude / OpenAI). Its job is to STOP hammering a provider
// that is already struggling: once enough consecutive "the provider is
// unhealthy" failures pile up, the breaker trips Open and rejects further
// calls instantly (fail-fast) instead of piling more load onto a service that
// is timing out, rate-limiting, or 5xx-ing. After a cool-down it lets a few
// probe calls through (Half-Open) to see whether the provider has recovered,
// and only fully re-enables traffic (Closed) once those probes succeed.
//
// It composes with — and sits ABOVE — the retry engine (see retry.go): retries
// smooth over a single hiccup, while the breaker protects against a sustained
// outage where retrying would only make things worse. A typical stack is:
//
//	CallAIWithCircuitBreaker(ctx, "claude", func(ctx) {
//	    return CallAIWithRetry(ctx, cfg, "anthropic-messages", roundTrip)
//	})
//
// so the breaker counts one failure only after retries are exhausted.
//
// State machine:
//
//	Closed  --(FailureThreshold consecutive counting failures)-->  Open
//	Open    --(ResetTimeout elapsed)-------------------------->  Half-Open
//	Half-Open --(SuccessThreshold consecutive probe successes)-->  Closed
//	Half-Open --(any counting failure OR HalfOpenTimeout)------->  Open

import (
	"context"
	"errors"
	"expvar"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dadiary/backend/internal/platform/httpx"
)

// ErrCircuitOpen is returned (wrapped) when the breaker rejects a call without
// ever invoking fn because the circuit is Open (or Half-Open at capacity).
// Callers can detect it with errors.Is to react — e.g. fall back to another
// provider — instead of surfacing it as a hard error.
var ErrCircuitOpen = errors.New("circuit breaker open: AI provider temporarily unavailable")

// State is the current position in the circuit breaker's three-state machine.
type State int32

const (
	// StateClosed is normal operation: calls flow through to the provider.
	StateClosed State = iota
	// StateOpen rejects calls instantly (fail-fast) after too many failures.
	StateOpen
	// StateHalfOpen lets a limited number of probe calls through to test recovery.
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig tunes when the breaker trips and recovers. Zero-valued
// fields fall back to sensible defaults via withDefaults, so a partial config
// (or the zero value) is safe.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of CONSECUTIVE counting failures (see
	// ShouldTrip) in the Closed state that trips the breaker Open. e.g. 5.
	FailureThreshold int
	// ResetTimeout is how long to stay Open before allowing Half-Open probes.
	// During this window every call is rejected instantly. e.g. 30s.
	ResetTimeout time.Duration
	// SuccessThreshold is the number of CONSECUTIVE probe successes in the
	// Half-Open state required to fully close the circuit again. e.g. 2.
	SuccessThreshold int
	// HalfOpenMaxCalls caps how many probe calls may be in flight concurrently
	// while Half-Open, so a burst of callers doesn't re-flood a fragile
	// provider the instant the cool-down ends. Defaults to SuccessThreshold.
	HalfOpenMaxCalls int
	// HalfOpenTimeout bounds how long the breaker may linger in Half-Open
	// without gathering enough successes; once it elapses the breaker reverts
	// to Open (assume the provider is still unhealthy). 0 disables the bound.
	HalfOpenTimeout time.Duration
	// ShouldTrip classifies whether a given error counts toward tripping the
	// breaker. It must count "the provider is unhealthy" failures (network,
	// 429 rate limit, 5xx) and IGNORE caller/permanent errors (context
	// cancellation, 400 bad request, 401/403 auth, quota exhaustion) — those
	// don't mean the provider is down, so they must not open the circuit.
	// Defaults to httpx.IsRetryable, which already draws exactly that line.
	ShouldTrip func(error) bool
	// Now is the clock, injectable for deterministic tests. Defaults to time.Now.
	Now func() time.Time
}

// DefaultCircuitBreakerConfig returns the recommended breaker policy for AI calls.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		ResetTimeout:     30 * time.Second,
		SuccessThreshold: 2,
		HalfOpenMaxCalls: 2,
		HalfOpenTimeout:  30 * time.Second,
		ShouldTrip:       httpx.IsRetryable,
		Now:              time.Now,
	}
}

// withDefaults fills any zero/invalid field from DefaultCircuitBreakerConfig.
func (c CircuitBreakerConfig) withDefaults() CircuitBreakerConfig {
	d := DefaultCircuitBreakerConfig()
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = d.FailureThreshold
	}
	if c.ResetTimeout <= 0 {
		c.ResetTimeout = d.ResetTimeout
	}
	if c.SuccessThreshold <= 0 {
		c.SuccessThreshold = d.SuccessThreshold
	}
	if c.HalfOpenMaxCalls <= 0 {
		// Allow exactly enough probes to satisfy SuccessThreshold by default.
		c.HalfOpenMaxCalls = c.SuccessThreshold
	}
	// HalfOpenTimeout may legitimately be 0 (disabled); leave it as-is.
	if c.ShouldTrip == nil {
		c.ShouldTrip = d.ShouldTrip
	}
	if c.Now == nil {
		c.Now = d.Now
	}
	return c
}

// CircuitBreaker is a thread-safe three-state breaker guarding one logical
// operation (e.g. all "claude" calls). Construct via NewCircuitBreaker.
type CircuitBreaker struct {
	name string
	cfg  CircuitBreakerConfig

	mu                   sync.Mutex
	state                State
	consecutiveFailures  int       // counting failures while Closed
	consecutiveSuccesses int       // probe successes while Half-Open
	openedAt             time.Time // when we last entered Open
	halfOpenSince        time.Time // when we last entered Half-Open
	halfOpenInFlight     int       // probe calls currently allowed/in flight
}

// NewCircuitBreaker builds a breaker for the named operation with the given
// config (zero-valued fields are defaulted). It starts Closed.
func NewCircuitBreaker(name string, cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		name:  name,
		cfg:   cfg.withDefaults(),
		state: StateClosed,
	}
}

// State returns the current breaker state (mainly for tests / observability).
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// beforeCall decides whether a call may proceed, performing any time-based
// state transition (Open->Half-Open on cool-down, Half-Open->Open on timeout)
// as a side effect. It returns ErrCircuitOpen when the call must be rejected.
// On a permitted Half-Open probe it reserves a slot (released in afterCall).
func (cb *CircuitBreaker) beforeCall() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := cb.cfg.Now()
	switch cb.state {
	case StateClosed:
		return nil

	case StateOpen:
		// Still cooling down? Fail fast.
		if now.Sub(cb.openedAt) < cb.cfg.ResetTimeout {
			return ErrCircuitOpen
		}
		// Cool-down elapsed: move to Half-Open and let this call probe.
		cb.toHalfOpen(now)
		cb.halfOpenInFlight++
		return nil

	case StateHalfOpen:
		// Stuck in Half-Open too long without recovering: assume still down.
		if cb.cfg.HalfOpenTimeout > 0 && now.Sub(cb.halfOpenSince) >= cb.cfg.HalfOpenTimeout {
			cb.toOpen(now)
			return ErrCircuitOpen
		}
		// Only a bounded number of probes are allowed at once.
		if cb.halfOpenInFlight >= cb.cfg.HalfOpenMaxCalls {
			return ErrCircuitOpen
		}
		cb.halfOpenInFlight++
		return nil

	default:
		return nil
	}
}

// afterCall records the outcome of a permitted call and transitions the state
// machine accordingly. err == nil is a success; a non-nil err only counts as a
// failure when cfg.ShouldTrip(err) is true (so caller/permanent errors neither
// trip the breaker nor count as recovery).
func (cb *CircuitBreaker) afterCall(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := cb.cfg.Now()
	success := err == nil
	countingFailure := err != nil && cb.cfg.ShouldTrip(err)

	switch cb.state {
	case StateClosed:
		if success {
			cb.consecutiveFailures = 0
			return
		}
		if countingFailure {
			cb.consecutiveFailures++
			if cb.consecutiveFailures >= cb.cfg.FailureThreshold {
				cb.toOpen(now)
			}
		}
		// Non-counting failures (e.g. context canceled, 400) are ignored.

	case StateHalfOpen:
		// Release the probe slot reserved in beforeCall.
		if cb.halfOpenInFlight > 0 {
			cb.halfOpenInFlight--
		}
		switch {
		case success:
			cb.consecutiveSuccesses++
			if cb.consecutiveSuccesses >= cb.cfg.SuccessThreshold {
				cb.toClosed()
			}
		case countingFailure:
			// A single real failure during recovery re-opens immediately.
			cb.toOpen(now)
		}
		// Non-counting failures during Half-Open keep us probing (no change).

	case StateOpen:
		// Calls in Open are normally rejected before fn runs, so reaching here
		// is unusual; nothing to record.
	}
}

// toOpen trips the breaker Open and resets counters. Caller holds cb.mu.
func (cb *CircuitBreaker) toOpen(now time.Time) {
	prev := cb.state
	cb.state = StateOpen
	cb.openedAt = now
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
	cb.halfOpenInFlight = 0
	if prev != StateOpen {
		metricCBOpened.Add(1)
		setCircuitState(cb.name, StateOpen)
		slog.Warn("circuit breaker opened",
			"op", cb.name,
			"from", prev.String(),
			"reset_timeout", cb.cfg.ResetTimeout.String(),
		)
	}
}

// toHalfOpen begins the probing phase. Caller holds cb.mu.
func (cb *CircuitBreaker) toHalfOpen(now time.Time) {
	prev := cb.state
	cb.state = StateHalfOpen
	cb.halfOpenSince = now
	cb.consecutiveSuccesses = 0
	cb.halfOpenInFlight = 0
	if prev != StateHalfOpen {
		setCircuitState(cb.name, StateHalfOpen)
		slog.Info("circuit breaker half-open (probing recovery)",
			"op", cb.name,
			"from", prev.String(),
			"success_threshold", cb.cfg.SuccessThreshold,
		)
	}
}

// toClosed fully re-enables traffic. Caller holds cb.mu.
func (cb *CircuitBreaker) toClosed() {
	prev := cb.state
	cb.state = StateClosed
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
	cb.halfOpenInFlight = 0
	if prev != StateClosed {
		setCircuitState(cb.name, StateClosed)
		slog.Info("circuit breaker closed (provider recovered)",
			"op", cb.name,
			"from", prev.String(),
		)
	}
}

// -----------------------------------------------------------------------------
// Per-operation registry + generic wrapper
// -----------------------------------------------------------------------------

var (
	breakerMu            sync.Mutex
	breakers             = map[string]*CircuitBreaker{}
	defaultBreakerConfig = DefaultCircuitBreakerConfig()
)

// SetDefaultCircuitBreakerConfig overrides the config used for breakers created
// lazily by CallAIWithCircuitBreaker from now on. Existing breakers keep their
// config. Call once at startup (before serving traffic) if you want non-default
// thresholds; passing the zero value restores the built-in defaults.
func SetDefaultCircuitBreakerConfig(cfg CircuitBreakerConfig) {
	breakerMu.Lock()
	defer breakerMu.Unlock()
	defaultBreakerConfig = cfg.withDefaults()
}

// breakerFor returns the breaker for op, creating it on first use. All calls
// sharing an op label share one breaker, so their health is tracked together.
func breakerFor(op string) *CircuitBreaker {
	breakerMu.Lock()
	defer breakerMu.Unlock()
	if b, ok := breakers[op]; ok {
		return b
	}
	b := NewCircuitBreaker(op, defaultBreakerConfig)
	breakers[op] = b
	return b
}

// CallAIWithCircuitBreaker runs fn guarded by the circuit breaker for op.
//
//   - If the breaker is Open (or Half-Open at capacity) the call is rejected
//     immediately with an error wrapping ErrCircuitOpen, and fn is NOT invoked.
//   - Otherwise fn runs and its outcome updates the breaker's state.
//
// op is the label that selects/creates the shared breaker (e.g. "claude",
// "openai"); use distinct labels per provider so one provider's outage never
// trips another's breaker. fn receives ctx so it honours cancellation.
//
// This is the circuit-breaking companion to CallAIWithRetry; the two compose,
// with the breaker on the outside so it counts a failure only after retries
// have been exhausted.
func CallAIWithCircuitBreaker[T any](ctx context.Context, op string, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	// Respect an already-cancelled caller before touching breaker state.
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	cb := breakerFor(op)
	if err := cb.beforeCall(); err != nil {
		metricCBRejected.Add(1)
		slog.Warn("circuit breaker rejected call (failing fast)",
			"op", op,
			"state", cb.State().String(),
		)
		return zero, fmt.Errorf("%s: %w", op, err)
	}

	val, err := fn(ctx)
	cb.afterCall(err)
	if err != nil {
		return zero, err
	}
	return val, nil
}

// -----------------------------------------------------------------------------
// Metrics (expvar; zero external deps, visible at /debug/vars)
// -----------------------------------------------------------------------------

// Metric names are exported so dashboards/alerts can reference them without
// hard-coding strings.
const (
	// MetricCircuitOpened counts how many times any breaker tripped Open.
	MetricCircuitOpened = "dadiary_circuit_opened_total"
	// MetricCircuitRejected counts calls rejected fast because a breaker was open.
	MetricCircuitRejected = "dadiary_circuit_rejected_total"
	// MetricCircuitState is a per-op gauge of the current state (string).
	MetricCircuitState = "dadiary_circuit_state"
)

var (
	metricCBOpened   = expvar.NewInt(MetricCircuitOpened)
	metricCBRejected = expvar.NewInt(MetricCircuitRejected)
	metricCBState    = expvar.NewMap(MetricCircuitState)
)

// setCircuitState publishes the current state for op into the expvar map.
func setCircuitState(op string, s State) {
	var v expvar.String
	v.Set(s.String())
	metricCBState.Set(op, &v)
}
