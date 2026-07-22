// Package alert provides a small ops-alert fan-out for Payment / Subscription monitoring.
//
// Design tradeoffs:
//   - Console slog ALWAYS runs (no cooldown) so local/Railway logs stay complete
//     even when DADIARY_ALERT_ENABLED=false or remote sinks are cooling down.
//   - Cooldown applies only to remote sinks (Slack / Telegram), and lastSent is
//     stamped ONLY after at least one sink returns 2xx. A failed sink does not
//     burn the 15m window — the next event can retry.
//   - Default bucket is per Key (e.g. all signature_invalid share one bucket) to
//     stop SePay-retry storms. Set Event.UniqueSuffix (e.g. invoice) when each
//     distinct event must notify (payment success).
//
// Remote delivery runs in a detached goroutine so IPN handlers never block.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Level is the severity of an ops alert.
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Well-known cooldown keys (shared anti-spam buckets unless UniqueSuffix is set).
const (
	KeySignatureInvalid   = "signature_invalid"
	KeyFulfillFailed      = "fulfill_failed"
	KeyUnknownStatus      = "unknown_status"
	KeyHighFailRate       = "high_fail_rate"
	KeyWebhookErrorsHigh  = "webhook_errors_high"
	KeyServiceUnavailable = "service_unavailable"
	KeyCancelFailed       = "cancel_failed"
	KeyOrderLookupFailed  = "order_lookup_failed"
	KeyJobFailed          = "plan_expiry_job_failed"
	KeyDowngradeSpike     = "plan_expiry_downgrade_spike"
	KeyLockClaimFailed    = "plan_expiry_lock_claim_failed"
	// KeyPaymentSuccess is per-invoice via UniqueSuffix (no shared spam bucket).
	KeyPaymentSuccess = "payment_success"
)

// DefaultCooldown is the per-bucket silence window after a successful remote send.
const DefaultCooldown = 15 * time.Minute

// remoteSendTimeout bounds detached Slack/Telegram HTTP calls.
const remoteSendTimeout = 8 * time.Second

// Event is a single alert payload.
type Event struct {
	// Key is the primary cooldown bucket (e.g. signature_invalid).
	// Empty → derived from Fields["reason"] or Title.
	Key string
	// UniqueSuffix optionally narrows the cooldown bucket (e.g. invoice number).
	// Tradeoff: empty = anti-spam (all events of Key share 15m); set = per-event
	// remote notify (use for payment_success). Does not affect console logging.
	UniqueSuffix string
	// Detail is optional human context appended to the message body only
	// (never part of the cooldown key).
	Detail  string
	Title   string         // short human title
	Level   Level          // info | warn | error
	Message string         // one-line summary
	Fields  map[string]any // structured context (user_id, order_id, …)
}

// Alerter delivers ops alerts. Implementations must be safe for concurrent use
// and must not panic on nil / empty events.
type Alerter interface {
	Send(ctx context.Context, e Event)
}

// Config holds optional remote sinks. Empty URLs → console-only.
type Config struct {
	// Enabled gates remote Slack/Telegram delivery only.
	// Console ops_alert lines always emit regardless of Enabled / cooldown.
	Enabled bool
	// WebhookURL is a Slack Incoming Webhook (or any endpoint accepting JSON).
	WebhookURL string
	// TelegramBotToken + TelegramChatID enable Telegram Bot API sendMessage.
	TelegramBotToken string
	TelegramChatID   string
	// HTTPClient overrides the default client (tests). Nil → timeout aligned with remoteSendTimeout.
	HTTPClient *http.Client
	// Cooldown overrides DefaultCooldown. Zero / negative → DefaultCooldown.
	Cooldown time.Duration
}

// Fanout sends to console + optional remote sinks.
type Fanout struct {
	cfg      Config
	client   *http.Client
	cooldown time.Duration

	mu       sync.Mutex
	lastSent map[string]time.Time // stamped only after remote success
	inFlight map[string]bool      // prevents duplicate concurrent remotes for same bucket
}

// New builds a Fanout alerter from config. Never returns nil.
func New(cfg Config) *Fanout {
	c := cfg.HTTPClient
	if c == nil {
		c = &http.Client{Timeout: remoteSendTimeout}
	}
	cd := cfg.Cooldown
	if cd <= 0 {
		cd = DefaultCooldown
	}
	return &Fanout{
		cfg:      cfg,
		client:   c,
		cooldown: cd,
		lastSent: make(map[string]time.Time),
		inFlight: make(map[string]bool),
	}
}

// Nop is a no-op Alerter (tests / unset wiring).
type Nop struct{}

// Send implements Alerter.
func (Nop) Send(context.Context, Event) {}

// Send implements Alerter.
//
// Flow:
//  1. Always log to console (no cooldown — operators keep full slog history).
//  2. If remote disabled / unconfigured → return (no cooldown state touched).
//  3. If bucket cooling or already in-flight → skip remote only.
//  4. Detached goroutine delivers remote; lastSent stamped only on success.
func (f *Fanout) Send(ctx context.Context, e Event) {
	if f == nil {
		return
	}
	if e.Level == "" {
		e.Level = LevelError
	}

	// Console is never suppressed by cooldown / Enabled.
	logConsole(e)

	if !f.cfg.Enabled {
		return
	}
	if !f.hasRemote() {
		return
	}

	key := e.cooldownKey()
	if !f.beginRemote(key) {
		slog.Debug("alert: remote suppressed by cooldown",
			"key", key,
			"title", e.Title,
			"cooldown", f.cooldown.String(),
		)
		return
	}

	// Detach from request ctx — IPN must ACK without waiting on Slack/Telegram.
	go f.sendRemoteDetached(e, key)
}

// Send is a nil-safe helper for optional Alerter deps (no-op when a is nil).
func Send(ctx context.Context, a Alerter, e Event) {
	if a == nil {
		return
	}
	a.Send(ctx, e)
}

func (f *Fanout) hasRemote() bool {
	return strings.TrimSpace(f.cfg.WebhookURL) != "" ||
		(strings.TrimSpace(f.cfg.TelegramBotToken) != "" && strings.TrimSpace(f.cfg.TelegramChatID) != "")
}

func (e Event) cooldownKey() string {
	base := strings.TrimSpace(e.Key)
	if base == "" && e.Fields != nil {
		if r, ok := e.Fields["reason"].(string); ok {
			base = strings.TrimSpace(r)
		}
	}
	if base == "" {
		base = strings.TrimSpace(e.Title)
	}
	if base == "" {
		base = "default"
	}
	if suf := strings.TrimSpace(e.UniqueSuffix); suf != "" {
		return base + "|" + suf
	}
	return base
}

// beginRemote reserves the bucket for an in-flight remote send.
// Returns false when still cooling down or another send is already in flight.
func (f *Fanout) beginRemote(key string) bool {
	now := time.Now().UTC()
	f.mu.Lock()
	defer f.mu.Unlock()
	if last, ok := f.lastSent[key]; ok && now.Sub(last) < f.cooldown {
		return false
	}
	if f.inFlight[key] {
		return false
	}
	f.inFlight[key] = true
	return true
}

// endRemote clears in-flight and stamps lastSent only when success is true.
func (f *Fanout) endRemote(key string, success bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.inFlight, key)
	if success {
		f.lastSent[key] = time.Now().UTC()
	}
}

// LastSentAt returns the last successful remote emit time for key (tests).
func (f *Fanout) LastSentAt(key string) (time.Time, bool) {
	if f == nil {
		return time.Time{}, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.lastSent[key]
	return t, ok
}

func (f *Fanout) sendRemoteDetached(e Event, key string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("alert: remote send panic", "recover", fmt.Sprint(r), "title", e.Title)
			f.endRemote(key, false)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), remoteSendTimeout)
	defer cancel()

	ok := f.deliverRemotes(ctx, e)
	f.endRemote(key, ok)
	if !ok {
		slog.Warn("alert: remote delivery failed — cooldown not stamped",
			"key", key,
			"title", e.Title,
		)
	}
}

// deliverRemotes posts to all configured sinks. Success = at least one 2xx.
func (f *Fanout) deliverRemotes(ctx context.Context, e Event) bool {
	anyOK := false
	attempted := false

	if url := strings.TrimSpace(f.cfg.WebhookURL); url != "" {
		attempted = true
		if f.postSlack(ctx, url, e) {
			anyOK = true
		}
	}
	token := strings.TrimSpace(f.cfg.TelegramBotToken)
	chat := strings.TrimSpace(f.cfg.TelegramChatID)
	if token != "" && chat != "" {
		attempted = true
		if f.postTelegram(ctx, token, chat, e) {
			anyOK = true
		}
	}
	return attempted && anyOK
}

func logConsole(e Event) {
	attrs := make([]any, 0, 6+len(e.Fields)*2)
	attrs = append(attrs, "alert_title", e.Title, "alert_level", string(e.Level))
	if e.Key != "" {
		attrs = append(attrs, "alert_key", e.Key)
	}
	if e.UniqueSuffix != "" {
		attrs = append(attrs, "alert_unique", e.UniqueSuffix)
	}
	if e.Message != "" {
		attrs = append(attrs, "message", e.Message)
	}
	if e.Detail != "" {
		attrs = append(attrs, "detail", e.Detail)
	}
	for k, v := range e.Fields {
		attrs = append(attrs, k, v)
	}
	msg := "ops_alert"
	if e.Title != "" {
		msg = "ops_alert: " + e.Title
	}
	switch e.Level {
	case LevelWarn:
		slog.Warn(msg, attrs...)
	case LevelInfo:
		slog.Info(msg, attrs...)
	default:
		slog.Error(msg, attrs...)
	}
}

func (f *Fanout) postSlack(ctx context.Context, url string, e Event) bool {
	text := formatText(e)
	body, err := json.Marshal(map[string]any{
		"text":   text,
		"level":  string(e.Level),
		"title":  e.Title,
		"key":    e.cooldownKey(),
		"fields": e.Fields,
	})
	if err != nil {
		slog.Error("alert: marshal slack body failed", "error", err.Error())
		return false
	}
	return f.doPOST(ctx, url, "application/json", body, "slack_webhook")
}

func (f *Fanout) postTelegram(ctx context.Context, token, chatID string, e Event) bool {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	body, err := json.Marshal(map[string]any{
		"chat_id": chatID,
		"text":    formatText(e),
	})
	if err != nil {
		slog.Error("alert: marshal telegram body failed", "error", err.Error())
		return false
	}
	return f.doPOST(ctx, apiURL, "application/json", body, "telegram")
}

func (f *Fanout) doPOST(ctx context.Context, url, contentType string, body []byte, sink string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Error("alert: build request failed", "sink", sink, "error", err.Error())
		return false
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := f.client.Do(req)
	if err != nil {
		slog.Error("alert: send failed", "sink", sink, "error", err.Error())
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		slog.Error("alert: sink non-2xx",
			"sink", sink,
			"status", resp.StatusCode,
		)
		return false
	}
	return true
}

func formatText(e Event) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[%s] %s", strings.ToUpper(string(e.Level)), e.Title))
	if e.Message != "" {
		b.WriteString("\n")
		b.WriteString(e.Message)
	}
	if e.Detail != "" {
		b.WriteString("\n")
		b.WriteString(e.Detail)
	}
	if len(e.Fields) > 0 {
		b.WriteString("\n")
		first := true
		for k, v := range e.Fields {
			if !first {
				b.WriteString(" · ")
			}
			first = false
			b.WriteString(fmt.Sprintf("%s=%v", k, v))
		}
	}
	return b.String()
}
