package ai

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/platform/httpx"
)

func TestAnalysisHealth_ThresholdCreditAndRecovery(t *testing.T) {
	resetAnalysisHealth()
	t.Cleanup(resetAnalysisHealth)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	NoteAnalysisFailure(errors.New("temporary timeout"))
	NoteAnalysisFailure(errors.New("temporary timeout"))
	if strings.Contains(buf.String(), LogAnalysisDown) {
		t.Fatalf("two failures should not announce an outage, log=%s", buf.String())
	}
	if AnalysisDown() {
		t.Fatal("tracker should stay up before the threshold")
	}

	NoteAnalysisFailure(errors.New("temporary timeout"))
	if n := strings.Count(buf.String(), LogAnalysisDown); n != 1 {
		t.Fatalf("threshold should log %s once, got %d in %s", LogAnalysisDown, n, buf.String())
	}
	if !AnalysisDown() {
		t.Fatal("tracker should be down after the threshold")
	}
	NoteAnalysisFailure(errors.New("temporary timeout"))
	if n := strings.Count(buf.String(), LogAnalysisDown); n != 1 {
		t.Fatalf("further failures should not log %s again, got %d", LogAnalysisDown, n)
	}

	NoteAnalysisSuccess()
	if n := strings.Count(buf.String(), LogAnalysisRecovered); n != 1 {
		t.Fatalf("success after an outage should log %s once, got %d in %s", LogAnalysisRecovered, n, buf.String())
	}
	if AnalysisDown() {
		t.Fatal("tracker should be up after recovery")
	}
	NoteAnalysisSuccess()
	if n := strings.Count(buf.String(), LogAnalysisRecovered); n != 1 {
		t.Fatalf("a healthy success should not log %s again, got %d", LogAnalysisRecovered, n)
	}

	buf.Reset()
	NoteAnalysisFailure(errors.New(`anthropic messages http 400: {"error":{"message":"Your credit balance is too low"}}`))
	if !strings.Contains(buf.String(), LogAnalysisDown) {
		t.Fatalf("a credit error should log %s immediately, log=%s", LogAnalysisDown, buf.String())
	}
	if !strings.Contains(buf.String(), "credit_or_quota=true") {
		t.Fatalf("credit outage log should mark credit_or_quota, log=%s", buf.String())
	}
	NoteAnalysisFailure(&httpx.StatusError{Provider: "openai chat", Status: 429, Body: `{"error":{"code":"credit_balance_exhausted"}}`})
	if n := strings.Count(buf.String(), LogAnalysisDown); n != 1 {
		t.Fatalf("credit errors while down should log %s once, got %d", LogAnalysisDown, n)
	}
	NoteAnalysisSuccess()
	if !strings.Contains(buf.String(), LogAnalysisRecovered) {
		t.Fatalf("success should log %s, log=%s", LogAnalysisRecovered, buf.String())
	}
}

func TestIsCreditOrQuotaAndProviderFailure(t *testing.T) {
	credit := []string{
		`anthropic messages http 400: Your credit balance is too low to access the Anthropic API`,
		`openai chat http 429: {"error":{"code":"credit_balance_exhausted"}}`,
		`{"type":"insufficient_quota"}`,
		`billing_hard_limit reached`,
	}
	for _, msg := range credit {
		if !IsCreditOrQuotaText(msg) {
			t.Errorf("want credit/quota: %s", msg)
		}
		if !IsProviderFailure(errors.New(msg)) {
			t.Errorf("want provider failure: %s", msg)
		}
	}
	if IsProviderFailure(errors.New("could not save completed analysis")) {
		t.Fatal("database save error is not a provider failure")
	}
	if IsCreditOrQuotaError(nil) || IsProviderFailure(nil) {
		t.Fatal("nil is not a provider failure")
	}
	wrapped := errors.New("text coach (skin-check): claude: empty; openai fallback: http 503")
	if !IsProviderFailure(wrapped) {
		t.Fatal("text coach fallback error should count as a provider failure")
	}
}
