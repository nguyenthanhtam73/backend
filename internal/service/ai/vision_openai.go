package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/platform/httpx"
	"github.com/dadiary/backend/internal/platform/imgprep"
	"github.com/dadiary/backend/internal/storage"
)

// VisionObservationPass uses OpenAI vision to extract conservative JSON observations (pass 1 of 2).
func VisionObservationPass(ctx context.Context, cfg *config.Config, httpClient *http.Client, store storage.Storage, relativeImagePaths []string) (string, error) {
	if cfg == nil || strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return "", fmt.Errorf("openai vision: missing api key")
	}
	if store == nil {
		return "", fmt.Errorf("openai vision: storage unavailable")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 4 * time.Minute}
	}
	model := cfg.OpenAIVisionModel()
	logVisionModelSelection("vision-observation", model)
	userText := VisionObservationSchemaBlock + "\n\nThe images are user skin check-in photo(s)."
	parts := []map[string]any{
		{"type": "text", "text": userText},
	}
	for _, rel := range relativeImagePaths {
		data, err := store.Read(ctx, rel)
		if err != nil {
			return "", fmt.Errorf("read image %s: %w", rel, err)
		}
		data, err = imgprep.LimitForVisionAPI(data)
		if err != nil {
			return "", fmt.Errorf("prepare image %s: %w", rel, err)
		}
		mime := "image/jpeg"
		b64 := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data))
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": b64,
			},
		})
	}
	body := map[string]any{
		"model":           model,
		"temperature":     0.2,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": VisionObservationSystemPrompt(),
			},
			{
				"role":    "user",
				"content": parts,
			},
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
	// Images are already read + resized above; retry only the HTTP round-trip.
	b, err := CallAIWithRetry(ctx, cfg, "openai-vision", func(ctx context.Context) ([]byte, error) {
		return httpx.PostJSON(ctx, httpClient, "openai vision", "https://api.openai.com/v1/chat/completions", headers, payload)
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("openai vision: empty content")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

// GPTSinglePassSkinCoach is the legacy multimodal path when Anthropic is not configured.
// skillLevel selects Beginner vs Normal coach persona via GetCoachPrompt.
func GPTSinglePassSkinCoach(ctx context.Context, cfg *config.Config, httpClient *http.Client, store storage.Storage, relativeImagePaths []string, userContextAndSchema string, skillLevel string) (string, error) {
	if cfg == nil || strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return "", fmt.Errorf("openai: missing api key")
	}
	if store == nil {
		return "", fmt.Errorf("openai: storage unavailable")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 6 * time.Minute}
	}
	model := strings.TrimSpace(cfg.OpenAI.VisionModel)
	if model == "" {
		model = strings.TrimSpace(cfg.OpenAI.Model)
	}
	if model == "" {
		model = "gpt-4o"
	}
	parts := []map[string]any{
		{"type": "text", "text": userContextAndSchema},
	}
	for _, rel := range relativeImagePaths {
		data, err := store.Read(ctx, rel)
		if err != nil {
			return "", fmt.Errorf("read image %s: %w", rel, err)
		}
		// Empty / non-image bytes shouldn't reach OpenAI — vision call would either
		// 400 or (worse) return an empty assistant message that breaks JSON parse later.
		if len(data) == 0 {
			return "", fmt.Errorf("image %s is empty (0 bytes)", rel)
		}
		data, err = imgprep.LimitForVisionAPI(data)
		if err != nil {
			return "", fmt.Errorf("prepare image %s: %w", rel, err)
		}
		mime := "image/jpeg"
		b64 := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data))
		parts = append(parts, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": b64},
		})
	}
	body := map[string]any{
		"model":           model,
		"temperature":     0.3,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]any{
			{"role": "system", "content": GetCoachPrompt(skillLevel)},
			{"role": "user", "content": parts},
		},
	}
	payload, _ := json.Marshal(body)
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.OpenAI.APIKey,
		"Content-Type":  "application/json",
	}
	b, err := CallAIWithRetry(ctx, cfg, "openai-coach", func(ctx context.Context) ([]byte, error) {
		return httpx.PostJSON(ctx, httpClient, "openai coach", "https://api.openai.com/v1/chat/completions", headers, payload)
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
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai coach: empty")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
