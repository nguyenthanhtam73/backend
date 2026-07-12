package ai

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
	// AnthropicMessages is only used for the text-coach pass, so resolve through
	// AnthropicCoachModel (honours the optional DADIARY_ANTHROPIC_FAST_MODEL toggle).
	model := cfg.AnthropicCoachModel()
	body := map[string]any{
		"model": model,
		// max_tokens is a safety ceiling on generation time, not the target length.
		// A full coach JSON is ~1.2–1.8k tokens, so 4096 leaves comfortable headroom
		// (no truncation) while still capping a pathological runaway generation at
		// roughly half the previous 8192 budget. Actual latency is reduced by the
		// brevity constraints in the prompt/schema, not by this cap.
		"max_tokens":  4096,
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
	headers := map[string]string{
		"x-api-key":         cfg.Anthropic.APIKey,
		"anthropic-version": "2023-06-01",
		"Content-Type":      "application/json",
	}
	// Retry transient failures (network, 429, 5xx) with backoff; the round-trip
	// is the only thing retried, so the payload is built exactly once above.
	b, err := CallAIWithRetry(ctx, cfg, "anthropic-messages", func(ctx context.Context) ([]byte, error) {
		return httpx.PostJSON(ctx, httpClient, "anthropic messages", anthropicMessagesURL, headers, payload)
	})
	if err != nil {
		return "", err
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
