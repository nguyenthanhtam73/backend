package retry

import "expvar"

// This file isolates the observability surface of the retry engine so the core
// control flow in retry.go stays readable, and so a future exporter (e.g.
// Prometheus) has a single place to hook into.
//
// All counters are published via expvar — part of the stdlib, zero extra
// dependencies. When the app mounts the default HTTP mux they are visible at
// /debug/vars; otherwise they are still readable in-process via Snapshot().

// Metric names are exported constants so dashboards, alerts, and a future
// Prometheus bridge can reference them without hard-coding strings.
const (
	// MetricTotal counts every retry performed (one per backoff), across all ops.
	MetricTotal = "dadiary_retry_total"
	// MetricSuccess counts operations that succeeded after >= 1 retry.
	MetricSuccess = "dadiary_retry_success"
	// MetricExhausted counts operations that failed after exhausting the budget.
	MetricExhausted = "dadiary_retry_exhausted"
	// MetricByOperation is a per-op breakdown of retries (op name -> count).
	MetricByOperation = "dadiary_retry_by_operation"
	// MetricLastError holds the most recent retryable error string (debug aid).
	MetricLastError = "dadiary_retry_last_error"
)

// Published expvar variables. expvar.New* panics on duplicate names, but each
// runs exactly once at package init, so registration is safe.
var (
	metricTotal       = expvar.NewInt(MetricTotal)
	metricSuccess     = expvar.NewInt(MetricSuccess)
	metricExhausted   = expvar.NewInt(MetricExhausted)
	metricByOperation = expvar.NewMap(MetricByOperation)
	metricLastError   = expvar.NewString(MetricLastError)
)

// opLabel normalises an empty operation name so metrics/maps stay tidy.
func opLabel(op string) string {
	if op == "" {
		return "unknown"
	}
	return op
}

// recordRetry is called once per retry (i.e. each time a transient failure is
// scheduled for another attempt). It bumps the global and per-op counters and
// remembers the triggering error for quick debugging.
func recordRetry(op string, err error) {
	metricTotal.Add(1)
	metricByOperation.Add(opLabel(op), 1)
	if err != nil {
		metricLastError.Set(err.Error())
	}
}

// recordSuccessAfterRetry is called when fn finally succeeds having been
// retried at least once.
func recordSuccessAfterRetry() {
	metricSuccess.Add(1)
}

// recordExhausted is called when the retry budget runs out. It keeps the final
// error visible via the last-error gauge.
func recordExhausted(err error) {
	metricExhausted.Add(1)
	if err != nil {
		metricLastError.Set(err.Error())
	}
}

// MetricsSnapshot is a point-in-time, dependency-free copy of the retry
// counters. Useful for tests, a /healthz style endpoint, or feeding a metrics
// exporter without exposing expvar internals.
type MetricsSnapshot struct {
	Total       int64            // total retries performed
	Success     int64            // successes after >= 1 retry
	Exhausted   int64            // budget-exhausted failures
	ByOperation map[string]int64 // retries per operation
	LastError   string           // most recent retryable error
}

// Snapshot reads the current metric values into a plain struct.
func Snapshot() MetricsSnapshot {
	s := MetricsSnapshot{
		Total:       metricTotal.Value(),
		Success:     metricSuccess.Value(),
		Exhausted:   metricExhausted.Value(),
		ByOperation: map[string]int64{},
		LastError:   metricLastError.Value(),
	}
	metricByOperation.Do(func(kv expvar.KeyValue) {
		if iv, ok := kv.Value.(*expvar.Int); ok {
			s.ByOperation[kv.Key] = iv.Value()
		}
	})
	return s
}

// resetMetrics clears all counters. Intended for tests that assert on metric
// deltas; not part of the public API.
func resetMetrics() {
	metricTotal.Set(0)
	metricSuccess.Set(0)
	metricExhausted.Set(0)
	metricByOperation.Init()
	metricLastError.Set("")
}
