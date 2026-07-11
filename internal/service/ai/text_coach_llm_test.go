package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/platform/openai"
	"github.com/dadiary/backend/pkg/retry"
)

func TestTextCoachCompletion_OpenAIOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	defer srv.Close()

	restore := openai.SetChatCompletionsURLForTest(srv.URL + "/v1/chat/completions")
	defer restore()

	cfg := &config.Config{
		OpenAI: config.OpenAIConfig{APIKey: "sk-test", Model: "gpt-4o"},
	}
	res, err := TextCoachCompletion(context.Background(), cfg, srv.Client(), "test", "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if res.Provider != TextCoachProviderOpenAI {
		t.Fatalf("provider=%q want openai", res.Provider)
	}
	if res.Model != "gpt-4o" {
		t.Fatalf("model=%q", res.Model)
	}
	if res.Fallback {
		t.Fatal("should not mark fallback when claude was not attempted")
	}
}

func TestTextCoachCompletion_ClaudeSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"provider\":\"claude\"}"}]}`))
	}))
	defer srv.Close()

	prevAnthropic := anthropicMessagesURL
	anthropicMessagesURL = srv.URL + "/v1/messages"
	defer func() { anthropicMessagesURL = prevAnthropic }()

	cfg := &config.Config{
		Anthropic: config.AnthropicConfig{APIKey: "sk-ant-test", Model: "claude-sonnet-4-20250514"},
		OpenAI:    config.OpenAIConfig{APIKey: "sk-test", Model: "gpt-4o"},
	}
	res, err := TextCoachCompletion(context.Background(), cfg, srv.Client(), "test", "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if res.Provider != TextCoachProviderClaude || res.Fallback {
		t.Fatalf("provider=%q fallback=%v", res.Provider, res.Fallback)
	}
	if res.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("model=%q", res.Model)
	}
}

func TestTextCoachCompletion_ClaudeThenOpenAIFallback(t *testing.T) {
	// Claude calls are now guarded by a package-level circuit breaker shared
	// across tests. Reset it so this test starts with a Closed breaker —
	// otherwise a breaker left Open by an earlier failing test (or a prior
	// -count iteration) would reject Claude instantly and skip the retries we
	// assert on here, making the test flaky.
	resetBreakersForTest()
	t.Cleanup(resetBreakersForTest)

	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/messages":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"overloaded"}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"from\":\"gpt\"}"}}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	prevAnthropic := anthropicMessagesURL
	anthropicMessagesURL = srv.URL + "/v1/messages"
	defer func() { anthropicMessagesURL = prevAnthropic }()
	restore := openai.SetChatCompletionsURLForTest(srv.URL + "/v1/chat/completions")
	defer restore()

	// Retry is a real, desired feature: Claude's 503 is a transient error, so
	// the retry engine attempts it several times BEFORE we give up and fall
	// back to OpenAI. That's why Claude is hit more than once (unlike the old
	// pre-retry expectation of a single Claude call + single OpenAI call).
	//
	// We pin an explicit, fast retry policy here so the attempt count is
	// deterministic (no flakiness from defaults changing or slow backoff):
	// maxAttempts = MaxRetries + 1 = 3 calls to Claude.
	const maxRetries = 2
	const claudeAttempts = maxRetries + 1
	cfg := &config.Config{
		Anthropic: config.AnthropicConfig{APIKey: "sk-ant-test", Model: "claude-sonnet-4-20250514"},
		OpenAI:    config.OpenAIConfig{APIKey: "sk-test", Model: "gpt-4o"},
		AI: config.AIConfig{Retry: retry.Config{
			MaxRetries:        maxRetries,
			InitialDelay:      time.Millisecond,
			MaxDelay:          5 * time.Millisecond,
			BackoffMultiplier: 2,
		}},
	}
	res, err := TextCoachCompletion(context.Background(), cfg, srv.Client(), "test", "sys", "user")
	if err != nil {
		t.Fatal(err)
	}

	// Assert the fallback *behaviour* rather than a hard-coded total count:
	// Claude is exhausted first (all its retries), then exactly one OpenAI call.
	if len(hits) < 2 {
		t.Fatalf("expected Claude retries then OpenAI fallback, got calls=%v", hits)
	}
	// Every Claude attempt must precede the single OpenAI fallback call.
	claudeCalls, openAICalls, sawOpenAI := 0, 0, false
	for _, path := range hits {
		switch path {
		case "/v1/messages":
			if sawOpenAI {
				t.Fatalf("Claude was called after OpenAI fallback; order=%v", hits)
			}
			claudeCalls++
		case "/v1/chat/completions":
			sawOpenAI = true
			openAICalls++
		}
	}
	if claudeCalls != claudeAttempts {
		t.Fatalf("claude calls=%d, want %d (initial attempt + %d retries)", claudeCalls, claudeAttempts, maxRetries)
	}
	if openAICalls != 1 {
		t.Fatalf("openai calls=%d, want exactly 1 fallback call", openAICalls)
	}

	// The final result must come from the OpenAI fallback path.
	if res.Provider != TextCoachProviderOpenAI || !res.Fallback {
		t.Fatalf("provider=%q fallback=%v", res.Provider, res.Fallback)
	}
	if !strings.Contains(res.Text, "gpt") {
		t.Fatalf("text=%q", res.Text)
	}
}

func TestTextCoachCompletion_ClaudeTimeoutFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/messages":
			w.WriteHeader(http.StatusGatewayTimeout)
			_, _ = w.Write([]byte(`{"error":"timeout"}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"from\":\"gpt-fallback\"}"}}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	prevAnthropic := anthropicMessagesURL
	anthropicMessagesURL = srv.URL + "/v1/messages"
	defer func() { anthropicMessagesURL = prevAnthropic }()
	restore := openai.SetChatCompletionsURLForTest(srv.URL + "/v1/chat/completions")
	defer restore()

	cfg := &config.Config{
		Anthropic: config.AnthropicConfig{APIKey: "sk-ant-test", Model: "claude-sonnet-4-20250514"},
		OpenAI:    config.OpenAIConfig{APIKey: "sk-test", Model: "gpt-4o"},
	}
	res, err := TextCoachCompletion(context.Background(), cfg, srv.Client(), "daily-feedback", "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Fallback || res.Provider != TextCoachProviderOpenAI {
		t.Fatalf("provider=%q fallback=%v", res.Provider, res.Fallback)
	}
}

func TestTextCoachCompletion_NoKeys(t *testing.T) {
	_, err := TextCoachCompletion(context.Background(), &config.Config{}, nil, "test", "sys", "user")
	if err == nil {
		t.Fatal("expected error without keys")
	}
}
