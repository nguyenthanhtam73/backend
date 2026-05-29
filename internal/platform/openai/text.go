// Package openai holds small, shared HTTP helpers for OpenAI Chat Completions (text-only).
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
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
		"model":           model,
		"temperature":     0.4,
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAI.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai chat http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
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
