// Package openai holds small, shared HTTP helpers for OpenAI Chat Completions (text-only).
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/platform/httpx"
)

var chatCompletionsURL = "https://api.openai.com/v1/chat/completions"

// SetChatCompletionsURLForTest overrides the OpenAI chat endpoint. Returns a restore func.
func SetChatCompletionsURLForTest(url string) func() {
	prev := chatCompletionsURL
	chatCompletionsURL = url
	return func() { chatCompletionsURL = prev }
}

// ChatCompletionJSON returns the assistant message content (expected JSON) for a text system+user turn.
// Uses cfg.OpenAI.Model (default gpt-4o) for text-coaching fallback — not the vision-only model.
func ChatCompletionJSON(ctx context.Context, cfg *config.Config, httpClient *http.Client, system, user string) (string, error) {
	if cfg == nil || strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return "", fmt.Errorf("openai: missing api key")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Minute}
	}
	model := strings.TrimSpace(cfg.OpenAI.Model)
	if model == "" {
		model = cfg.OpenAITextModel()
	}
	body := map[string]any{
		"model":       model,
		"temperature": 0.4,
		// Safety ceiling on the fallback coach JSON (mirrors the Claude cap). The
		// coach output is ~1.2–1.8k tokens, so 4096 avoids truncation while bounding
		// a runaway generation on the (rarely hit) OpenAI fallback path.
		"max_tokens":      4096,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]any{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.OpenAI.APIKey,
		"Content-Type":  "application/json",
	}
	// Retry transient failures (network, 429, 5xx) with backoff + jitter.
	b, err := httpx.WithRetry(ctx, cfg.AI.Retry, "openai-chat", func(ctx context.Context) ([]byte, error) {
		return httpx.PostJSON(ctx, httpClient, "openai chat", chatCompletionsURL, headers, payload)
	})
	if err != nil {
		return "", err
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("openai: empty completion")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
