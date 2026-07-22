package payment

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/pkg/alert"
	"github.com/google/uuid"
)

const (
	// failRateWindow is the rolling window for payment fail-rate alerts.
	failRateWindow = time.Hour
	// failRateThreshold triggers when failures / (success+fail) exceeds this.
	failRateThreshold = 0.10
	// failRateMinSamples avoids noisy alerts on tiny sample sizes.
	failRateMinSamples = 5
	// webhookErrorAlertThreshold fires when webhook errors in the hour exceed this.
	webhookErrorAlertThreshold = 5
)

// Monitor tracks recent payment outcomes for fail-rate / webhook-error alerts
// and optionally persists ops events for admin metrics.
// Alert storms are deduped by pkg/alert Fanout cooldown (per key, 15m).
type Monitor struct {
	alerter alert.Alerter
	ops     *repository.PaymentOpsEventRepository

	mu          sync.Mutex
	successes   []time.Time
	failures    []time.Time
	webhookErrs []time.Time
}

// NewMonitor constructs a health monitor. ops may be nil (in-memory only).
func NewMonitor(alerter alert.Alerter, ops *repository.PaymentOpsEventRepository) *Monitor {
	return &Monitor{alerter: alerter, ops: ops}
}

// RecordSuccess notes a successful fulfill (ORDER_PAID applied).
func (m *Monitor) RecordSuccess(ctx context.Context, invoice string) {
	if m == nil {
		return
	}
	now := time.Now().UTC()
	m.mu.Lock()
	m.successes = append(m.successes, now)
	m.pruneLocked(now)
	m.mu.Unlock()

	m.persist(ctx, domain.OpsKindPaymentSuccess, "fulfill_success", invoice)
	m.evaluate(ctx)
}

// RecordFailure notes a payment processing failure (amount mismatch, fulfill error, …).
func (m *Monitor) RecordFailure(ctx context.Context, reason, invoice string) {
	if m == nil {
		return
	}
	now := time.Now().UTC()
	m.mu.Lock()
	m.failures = append(m.failures, now)
	m.pruneLocked(now)
	m.mu.Unlock()

	m.persist(ctx, domain.OpsKindPaymentFail, reason, invoice)
	m.evaluate(ctx)
}

// RecordWebhookError notes a high-signal webhook failure (signature, unknown status, 5xx).
//
// Counted once: only webhookErrs (+ one ops row). Fail-rate includes webhookErrs
// in evaluate — do NOT also append to failures / persist payment_fail (that was
// double-counting the same IPN).
func (m *Monitor) RecordWebhookError(ctx context.Context, reason, invoice string) {
	if m == nil {
		return
	}
	now := time.Now().UTC()
	m.mu.Lock()
	m.webhookErrs = append(m.webhookErrs, now)
	m.pruneLocked(now)
	m.mu.Unlock()

	m.persist(ctx, domain.OpsKindWebhookError, reason, invoice)
	m.evaluate(ctx)
}

// Snapshot1h returns in-memory counts for the last hour (tests / debug).
func (m *Monitor) Snapshot1h() (successes, failures, webhookErrs int) {
	if m == nil {
		return 0, 0, 0
	}
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(now)
	return len(m.successes), len(m.failures), len(m.webhookErrs)
}

func (m *Monitor) persist(ctx context.Context, kind, reason, invoice string) {
	if m.ops == nil {
		return
	}
	ev := &domain.PaymentOpsEvent{
		ID:            uuid.New(),
		Kind:          kind,
		Reason:        reason,
		InvoiceNumber: invoice,
		CreatedAt:     time.Now().UTC(),
	}
	if err := m.ops.Create(ctx, ev); err != nil {
		slog.Warn("payment_monitor: persist ops event failed",
			"kind", kind,
			"reason", reason,
			"error", err.Error(),
		)
	}
}

func (m *Monitor) pruneLocked(now time.Time) {
	cut := now.Add(-failRateWindow)
	m.successes = filterAfter(m.successes, cut)
	m.failures = filterAfter(m.failures, cut)
	m.webhookErrs = filterAfter(m.webhookErrs, cut)
}

func filterAfter(in []time.Time, cut time.Time) []time.Time {
	if len(in) == 0 {
		return in
	}
	out := in[:0]
	for _, t := range in {
		if t.After(cut) || t.Equal(cut) {
			out = append(out, t)
		}
	}
	return out
}

func (m *Monitor) evaluate(ctx context.Context) {
	now := time.Now().UTC()
	m.mu.Lock()
	m.pruneLocked(now)
	okN := len(m.successes)
	// Webhook errors count once toward fail-rate (not also stored in failures).
	failN := len(m.failures) + len(m.webhookErrs)
	whN := len(m.webhookErrs)
	m.mu.Unlock()

	total := okN + failN
	if total >= failRateMinSamples {
		rate := float64(failN) / float64(total)
		if rate > failRateThreshold {
			pct := rate * 100
			slog.Error("payment_monitor: fail rate high",
				"fail_rate_pct", pct,
				"failures", failN,
				"webhook_errors", whN,
				"successes", okN,
				"window", failRateWindow.String(),
			)
			// Remote cooldown key high_fail_rate — Fanout stamps only on sink OK.
			alert.Send(ctx, m.alerter, alert.Event{
				Key:     alert.KeyHighFailRate,
				Title:   "Payment fail rate high",
				Level:   alert.LevelError,
				Message: "payment fail rate exceeded 10% in the last hour",
				Fields: map[string]any{
					"reason":         alert.KeyHighFailRate,
					"fail_rate_pct":  pct,
					"failures":       failN,
					"webhook_errors": whN,
					"successes":      okN,
					"window":         failRateWindow.String(),
					"threshold_pct":  failRateThreshold * 100,
				},
			})
		}
	}

	if whN > webhookErrorAlertThreshold {
		slog.Error("payment_monitor: webhook errors high",
			"webhook_errors", whN,
			"window", failRateWindow.String(),
		)
		alert.Send(ctx, m.alerter, alert.Event{
			Key:     alert.KeyWebhookErrorsHigh,
			Title:   "SePay webhook errors high",
			Level:   alert.LevelError,
			Message: "webhook errors exceeded threshold in the last hour",
			Fields: map[string]any{
				"reason":         alert.KeyWebhookErrorsHigh,
				"webhook_errors": whN,
				"threshold":      webhookErrorAlertThreshold,
				"window":         failRateWindow.String(),
			},
		})
	}
}
