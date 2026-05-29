package ai

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

// anthropicMessagesURL is the Anthropic Messages API endpoint (overridable in tests).
var anthropicMessagesURL = "https://api.anthropic.com/v1/messages"

// AnthropicMessages calls the Messages API (Claude) and returns concatenated text blocks from the assistant.
func AnthropicMessages(ctx context.Context, cfg *config.Config, httpClient *http.Client, system, user string) (string, error) {
	if cfg == nil || strings.TrimSpace(cfg.Anthropic.APIKey) == "" {
		return "", fmt.Errorf("anthropic: missing api key")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 6 * time.Minute}
	}
	model := strings.TrimSpace(cfg.Anthropic.Model)
	if model == "" {
		model = cfg.AnthropicModel()
	}
	body := map[string]any{
		"model":       model,
		"max_tokens":  8192,
		"temperature": 0.25,
		"system":      system,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": user},
				},
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicMessagesURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", cfg.Anthropic.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("anthropic messages http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return "", err
	}
	var out strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" && block.Text != "" {
			out.WriteString(block.Text)
		}
	}
	s := strings.TrimSpace(out.String())
	if s == "" {
		return "", fmt.Errorf("anthropic: empty assistant text")
	}
	return s, nil
}
