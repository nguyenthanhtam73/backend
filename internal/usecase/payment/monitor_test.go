package payment

import (
	"context"
	"sync"
	"testing"

	"github.com/dadiary/backend/pkg/alert"
)

type recordingAlerter struct {
	mu     sync.Mutex
	events []alert.Event
}

func (r *recordingAlerter) Send(_ context.Context, e alert.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingAlerter) titles() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Title
	}
	return out
}

func TestMonitorWebhookErrorThreshold(t *testing.T) {
	a := &recordingAlerter{}
	m := NewMonitor(a, nil)
	ctx := context.Background()

	// Seed successes so fail-rate stays quiet while we climb webhook errors.
	for i := 0; i < 40; i++ {
		m.RecordSuccess(ctx, "ok")
	}
	for i := 0; i < webhookErrorAlertThreshold; i++ {
		m.RecordWebhookError(ctx, "signature_invalid", "inv")
	}
	for _, title := range a.titles() {
		if title == "SePay webhook errors high" {
			t.Fatalf("unexpected webhook alert before crossing threshold")
		}
	}

	m.RecordWebhookError(ctx, "signature_invalid", "inv")
	found := false
	for _, title := range a.titles() {
		if title == "SePay webhook errors high" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing webhook alert in %v", a.titles())
	}
}

func TestMonitorFailRateThreshold(t *testing.T) {
	a := &recordingAlerter{}
	m := NewMonitor(a, nil)
	ctx := context.Background()

	// 1 success + 5 failures = 83% fail rate (>10%) with min samples.
	m.RecordSuccess(ctx, "ok-1")
	for i := 0; i < 5; i++ {
		m.RecordFailure(ctx, "amount_mismatch", "bad")
	}

	titles := a.titles()
	found := false
	for _, title := range titles {
		if title == "Payment fail rate high" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected fail-rate alert, got %v", titles)
	}
}

func TestRecordWebhookErrorCountsOnce(t *testing.T) {
	m := NewMonitor(nil, nil)
	ctx := context.Background()
	m.RecordWebhookError(ctx, "signature_invalid", "inv-1")
	m.RecordWebhookError(ctx, "signature_invalid", "inv-2")

	ok, fail, wh := m.Snapshot1h()
	if ok != 0 || fail != 0 {
		t.Fatalf("webhook errors must not land in failures slice: ok=%d fail=%d", ok, fail)
	}
	if wh != 2 {
		t.Fatalf("webhook_errors=%d", wh)
	}
}
