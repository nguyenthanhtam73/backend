package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitHits(hits *atomic.Int32, want int32, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if hits.Load() >= want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return hits.Load() >= want
}

func TestFanoutPostsSlackWhenEnabled(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("json: %v", err)
		}
		if payload["title"] != "SePay webhook: signature_invalid" {
			t.Errorf("title=%v", payload["title"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	a := New(Config{
		Enabled:    true,
		WebhookURL: srv.URL,
		HTTPClient: srv.Client(),
		Cooldown:   time.Minute,
	})
	a.Send(context.Background(), Event{
		Key:     KeySignatureInvalid,
		Title:   "SePay webhook: signature_invalid",
		Level:   LevelError,
		Message: "invalid sepay secret",
		Fields:  map[string]any{"reason": "signature_invalid"},
	})

	if !waitHits(&hits, 1, 2*time.Second) {
		t.Fatalf("expected 1 webhook hit, got %d", hits.Load())
	}
	if _, ok := a.LastSentAt(KeySignatureInvalid); !ok {
		t.Fatal("expected lastSent stamped after success")
	}
}

func TestFanoutCooldownSuppressesSameKey(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	a := New(Config{
		Enabled:    true,
		WebhookURL: srv.URL,
		HTTPClient: srv.Client(),
		Cooldown:   time.Hour,
	})
	ctx := context.Background()
	ev := Event{Key: KeySignatureInvalid, Title: "sig", Level: LevelError}
	a.Send(ctx, ev)
	if !waitHits(&hits, 1, 2*time.Second) {
		t.Fatal("first send never landed")
	}
	// After success, further sends in the same bucket must be suppressed.
	a.Send(ctx, ev)
	a.Send(ctx, ev)
	time.Sleep(80 * time.Millisecond)
	if hits.Load() != 1 {
		t.Fatalf("expected 1 hit under cooldown, got %d", hits.Load())
	}
}

func TestFanoutFailedSinkDoesNotStampCooldown(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	a := New(Config{
		Enabled:    true,
		WebhookURL: srv.URL,
		HTTPClient: srv.Client(),
		Cooldown:   time.Hour,
	})
	ctx := context.Background()
	ev := Event{Key: KeySignatureInvalid, Title: "sig", Level: LevelError}

	a.Send(ctx, ev)
	if !waitHits(&hits, 1, 2*time.Second) {
		t.Fatal("first attempt missing")
	}
	if _, ok := a.LastSentAt(KeySignatureInvalid); ok {
		t.Fatal("cooldown must not stamp on sink failure")
	}

	// Retry should be allowed because cooldown was not consumed.
	a.Send(ctx, ev)
	if !waitHits(&hits, 2, 2*time.Second) {
		t.Fatalf("expected retry after failed sink, hits=%d", hits.Load())
	}
}

func TestFanoutEnabledFalseSkipsRemoteAndCooldown(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	a := New(Config{
		Enabled:    false,
		WebhookURL: srv.URL,
		HTTPClient: srv.Client(),
		Cooldown:   time.Hour,
	})
	ctx := context.Background()
	// Multiple sends: console would log each time; remote/cooldown untouched.
	a.Send(ctx, Event{Key: KeySignatureInvalid, Title: "a", Level: LevelError})
	a.Send(ctx, Event{Key: KeySignatureInvalid, Title: "b", Level: LevelError})
	time.Sleep(80 * time.Millisecond)
	if hits.Load() != 0 {
		t.Fatalf("expected 0 remote hits when disabled, got %d", hits.Load())
	}
	if _, ok := a.LastSentAt(KeySignatureInvalid); ok {
		t.Fatal("enabled=false must not stamp cooldown")
	}
}

func TestFanoutUniqueSuffixSeparateBuckets(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	a := New(Config{
		Enabled:    true,
		WebhookURL: srv.URL,
		HTTPClient: srv.Client(),
		Cooldown:   time.Hour,
	})
	ctx := context.Background()
	a.Send(ctx, Event{Key: KeyPaymentSuccess, UniqueSuffix: "inv-1", Title: "ok1", Level: LevelInfo})
	a.Send(ctx, Event{Key: KeyPaymentSuccess, UniqueSuffix: "inv-2", Title: "ok2", Level: LevelInfo})

	if !waitHits(&hits, 2, 2*time.Second) {
		t.Fatalf("expected 2 hits for distinct UniqueSuffix, got %d", hits.Load())
	}
}

func TestFanoutDifferentKeysNotSuppressed(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	a := New(Config{
		Enabled:    true,
		WebhookURL: srv.URL,
		HTTPClient: srv.Client(),
		Cooldown:   time.Hour,
	})
	ctx := context.Background()
	a.Send(ctx, Event{Key: KeySignatureInvalid, Title: "a", Level: LevelError})
	a.Send(ctx, Event{Key: KeyFulfillFailed, Title: "b", Level: LevelError})

	if !waitHits(&hits, 2, 2*time.Second) {
		t.Fatalf("expected 2 hits for different keys, got %d", hits.Load())
	}
}

func TestSendNilAlerterNoPanic(t *testing.T) {
	t.Parallel()
	Send(context.Background(), nil, Event{Title: "noop", Level: LevelError})
}

func TestFanoutDoesNotBlockOnSlowSink(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	a := New(Config{
		Enabled:    true,
		WebhookURL: srv.URL,
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
		Cooldown:   time.Minute,
	})

	begin := time.Now()
	a.Send(context.Background(), Event{Key: "slow", Title: "slow", Level: LevelError})
	elapsed := time.Since(begin)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("Send blocked for %s — expected immediate return", elapsed)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("remote send never started")
	}
}

// paymentSuccessFixture is the ops payload shape from payment.paymentSuccessAlert.
// Slack's text field and Telegram's text field are both formatText of this event.
func paymentSuccessFixture() Event {
	const (
		userID  = "11111111-1111-1111-1111-111111111111"
		invoice = "DD-PAY-CODE-1"
	)
	return Event{
		Key:          KeyPaymentSuccess,
		UniqueSuffix: invoice,
		Title:        "Payment success",
		Level:        LevelInfo,
		Message: fmt.Sprintf(
			"User %s nâng cấp %s thành công, amount %d VND (%s), invoice %s",
			userID, "premium", 99000, "monthly", invoice,
		),
		Detail: "invoice=" + invoice,
		Fields: map[string]any{
			"reason":   KeyPaymentSuccess,
			"user_id":  userID,
			"plan":     "premium",
			"amount":   int64(99000),
			"interval": "monthly",
			"invoice":  invoice,
			"order_id": "22222222-2222-2222-2222-222222222222",
		},
	}
}

func TestFormatTextPaymentSuccessOmitsEmail(t *testing.T) {
	t.Parallel()

	const (
		rawEmail = "payer@gmail.com"
		userID   = "11111111-1111-1111-1111-111111111111"
		invoice  = "DD-PAY-CODE-1"
	)
	ev := paymentSuccessFixture()
	text := formatText(ev)
	assertPaymentSuccessText(t, text, userID, invoice, rawEmail, ev.Message)
	if _, ok := ev.Fields["user"]; ok {
		t.Fatal("user field must not be sent")
	}

	var slackBody []byte
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(slack.Close)

	f := New(Config{HTTPClient: slack.Client()})
	if !f.postSlack(context.Background(), slack.URL, ev) {
		t.Fatal("slack post failed")
	}
	var payload map[string]any
	if err := json.Unmarshal(slackBody, &payload); err != nil {
		t.Fatal(err)
	}
	slackText, _ := payload["text"].(string)
	assertPaymentSuccessText(t, slackText, userID, invoice, rawEmail, ev.Message)
	raw, _ := json.Marshal(payload["fields"])
	if strings.Contains(string(raw), rawEmail) || strings.Contains(string(raw), "@") {
		t.Fatalf("email in slack fields: %s", raw)
	}
	fields, _ := payload["fields"].(map[string]any)
	if fields["user_id"] != userID || fields["invoice"] != invoice {
		t.Fatalf("slack fields=%v", fields)
	}

	tgBody, err := telegramBody("ops", ev)
	if err != nil {
		t.Fatal(err)
	}
	var tg map[string]any
	if err := json.Unmarshal(tgBody, &tg); err != nil {
		t.Fatal(err)
	}
	tgText, _ := tg["text"].(string)
	assertPaymentSuccessText(t, tgText, userID, invoice, rawEmail, ev.Message)
}

func assertPaymentSuccessText(t *testing.T, text, userID, invoice, rawEmail, message string) {
	t.Helper()
	if !strings.Contains(text, message) || !strings.Contains(text, userID) || !strings.Contains(text, invoice) {
		t.Fatalf("slack/telegram text missing user_id, invoice, or message:\n%s", text)
	}
	if strings.Contains(text, rawEmail) || strings.Contains(text, "@") {
		t.Fatalf("email in slack/telegram text:\n%s", text)
	}
}
