package alert

import (
	"context"
	"strings"
	"sync"
)

// Recorder wraps an Alerter and keeps an in-memory ring of recent events.
// Used by Playwright E2E (DADIARY_E2E_SECRET) to assert payment-success /
// webhook alerts without calling real Telegram. Thread-safe.
type Recorder struct {
	inner Alerter
	mu    sync.Mutex
	log   []Event
	// max keeps memory bounded across long-running smoke runs.
	max int
}

// NewRecorder tee's every Send to inner (may be nil / Nop) and stores a copy.
func NewRecorder(inner Alerter) *Recorder {
	return &Recorder{inner: inner, max: 200}
}

// Send implements Alerter.
func (r *Recorder) Send(ctx context.Context, e Event) {
	if r == nil {
		return
	}
	cp := cloneEvent(e)
	r.mu.Lock()
	r.log = append(r.log, cp)
	if len(r.log) > r.max {
		// Drop oldest.
		r.log = append([]Event(nil), r.log[len(r.log)-r.max:]...)
	}
	r.mu.Unlock()

	if r.inner != nil {
		r.inner.Send(ctx, e)
	}
}

// Snapshot returns a copy of recorded events (oldest → newest).
func (r *Recorder) Snapshot() []Event {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.log))
	copy(out, r.log)
	return out
}

// Clear wipes the buffer (call between E2E cases when needed).
func (r *Recorder) Clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.log = nil
	r.mu.Unlock()
}

// Find filters recorded events. Empty key / invoice match anything.
func (r *Recorder) Find(key, invoice string) []Event {
	all := r.Snapshot()
	if key == "" && invoice == "" {
		return all
	}
	out := make([]Event, 0, len(all))
	for _, e := range all {
		if key != "" && e.cooldownKeyBase() != key && e.Key != key {
			continue
		}
		if invoice != "" {
			suf := strings.TrimSpace(e.UniqueSuffix)
			invField, _ := e.Fields["invoice"].(string)
			if suf != invoice && invField != invoice {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

func (e Event) cooldownKeyBase() string {
	base := strings.TrimSpace(e.Key)
	if base == "" && e.Fields != nil {
		if reason, ok := e.Fields["reason"].(string); ok {
			base = strings.TrimSpace(reason)
		}
	}
	return base
}

func cloneEvent(e Event) Event {
	cp := e
	if e.Fields != nil {
		cp.Fields = make(map[string]any, len(e.Fields))
		for k, v := range e.Fields {
			cp.Fields[k] = v
		}
	}
	return cp
}
