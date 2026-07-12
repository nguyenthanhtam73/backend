package retry

import (
	"context"
	"testing"
	"time"
)

// metricsTestConfig keeps backoff tiny so metric assertions run fast.
func metricsTestConfig() Config {
	return Config{
		MaxRetries:        3,
		InitialDelay:      time.Millisecond,
		MaxDelay:          5 * time.Millisecond,
		BackoffMultiplier: 2,
	}
}

// A clean first-attempt success must not touch any retry counters.
func TestMetrics_SuccessFirstTryRecordsNothing(t *testing.T) {
	resetMetrics()
	_, _ = DoValue(context.Background(), metricsTestConfig(), "op-a", retryAll, func(context.Context) (int, error) {
		return 1, nil
	})
	snap := Snapshot()
	if snap.Total != 0 || snap.Success != 0 || snap.Exhausted != 0 {
		t.Fatalf("expected all-zero metrics, got %+v", snap)
	}
	if len(snap.ByOperation) != 0 {
		t.Fatalf("expected empty by-operation map, got %+v", snap.ByOperation)
	}
}

// Success after two transient failures: Total counts the two retries, Success
// increments once, and the per-op breakdown attributes both retries to the op.
func TestMetrics_SuccessAfterRetries(t *testing.T) {
	resetMetrics()
	var calls int
	_, err := DoValue(context.Background(), metricsTestConfig(), "openai-vision", retryAll, func(context.Context) (int, error) {
		calls++
		if calls < 3 {
			return 0, errTransient
		}
		return 42, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snap := Snapshot()
	if snap.Total != 2 {
		t.Errorf("Total = %d, want 2 (two retries)", snap.Total)
	}
	if snap.Success != 1 {
		t.Errorf("Success = %d, want 1", snap.Success)
	}
	if snap.Exhausted != 0 {
		t.Errorf("Exhausted = %d, want 0", snap.Exhausted)
	}
	if snap.ByOperation["openai-vision"] != 2 {
		t.Errorf("ByOperation[openai-vision] = %d, want 2", snap.ByOperation["openai-vision"])
	}
	if snap.LastError != errTransient.Error() {
		t.Errorf("LastError = %q, want %q", snap.LastError, errTransient.Error())
	}
}

// Exhausting the budget records MaxRetries retries + one exhausted event, and
// keeps the final error visible in the last-error gauge.
func TestMetrics_Exhausted(t *testing.T) {
	resetMetrics()
	cfg := metricsTestConfig() // MaxRetries=3
	_, err := DoValue(context.Background(), cfg, "anthropic-messages", retryAll, func(context.Context) (int, error) {
		return 0, errTransient
	})
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
	snap := Snapshot()
	if snap.Total != 3 {
		t.Errorf("Total = %d, want 3 retries before exhaustion", snap.Total)
	}
	if snap.Exhausted != 1 {
		t.Errorf("Exhausted = %d, want 1", snap.Exhausted)
	}
	if snap.Success != 0 {
		t.Errorf("Success = %d, want 0", snap.Success)
	}
	if snap.ByOperation["anthropic-messages"] != 3 {
		t.Errorf("ByOperation[anthropic-messages] = %d, want 3", snap.ByOperation["anthropic-messages"])
	}
}

// A non-retryable error must not touch retry counters (fails fast).
func TestMetrics_NonRetryableRecordsNothing(t *testing.T) {
	resetMetrics()
	_, _ = DoValue(context.Background(), metricsTestConfig(), "op-x", retryNone, func(context.Context) (int, error) {
		return 0, errTransient
	})
	snap := Snapshot()
	if snap.Total != 0 || snap.Exhausted != 0 || snap.Success != 0 {
		t.Fatalf("non-retryable error should record nothing, got %+v", snap)
	}
}

// An empty operation name is bucketed under "unknown".
func TestMetrics_EmptyOpBucketedAsUnknown(t *testing.T) {
	resetMetrics()
	_, _ = DoValue(context.Background(), metricsTestConfig(), "", retryAll, func(context.Context) (int, error) {
		return 0, errTransient
	})
	snap := Snapshot()
	if snap.ByOperation["unknown"] == 0 {
		t.Fatalf("expected retries bucketed under \"unknown\", got %+v", snap.ByOperation)
	}
}

// Per-op counts must stay separated across operations.
func TestMetrics_ByOperationSeparation(t *testing.T) {
	resetMetrics()
	run := func(op string, failTimes int) {
		var calls int
		_, _ = DoValue(context.Background(), metricsTestConfig(), op, retryAll, func(context.Context) (int, error) {
			calls++
			if calls <= failTimes {
				return 0, errTransient
			}
			return 1, nil
		})
	}
	run("op-1", 1) // 1 retry
	run("op-2", 2) // 2 retries

	snap := Snapshot()
	if snap.ByOperation["op-1"] != 1 {
		t.Errorf("op-1 retries = %d, want 1", snap.ByOperation["op-1"])
	}
	if snap.ByOperation["op-2"] != 2 {
		t.Errorf("op-2 retries = %d, want 2", snap.ByOperation["op-2"])
	}
	if snap.Total != 3 {
		t.Errorf("Total = %d, want 3", snap.Total)
	}
}
