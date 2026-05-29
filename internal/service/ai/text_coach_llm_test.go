package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/platform/openai"
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

	cfg := &config.Config{
		Anthropic: config.AnthropicConfig{APIKey: "sk-ant-test", Model: "claude-sonnet-4-20250514"},
		OpenAI:    config.OpenAIConfig{APIKey: "sk-test", Model: "gpt-4o"},
	}
	res, err := TextCoachCompletion(context.Background(), cfg, srv.Client(), "test", "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0] != "/v1/messages" || hits[1] != "/v1/chat/completions" {
		t.Fatalf("call order=%v", hits)
	}
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
