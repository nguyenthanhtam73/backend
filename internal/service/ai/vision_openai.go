package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
)

// VisionObservationPass uses OpenAI vision to extract conservative JSON observations (pass 1 of 2).
func VisionObservationPass(ctx context.Context, cfg *config.Config, httpClient *http.Client, uploadRoot string, relativeImagePaths []string) (string, error) {
	if cfg == nil || strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return "", fmt.Errorf("openai vision: missing api key")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 4 * time.Minute}
	}
	model := strings.TrimSpace(cfg.OpenAI.VisionModel)
	if model == "" {
		model = strings.TrimSpace(cfg.OpenAI.Model)
	}
	if model == "" {
		model = "gpt-4o"
	}
	userText := VisionObservationSchemaBlock + "\n\nThe images are user skin check-in photo(s)."
	parts := []map[string]any{
		{"type": "text", "text": userText},
	}
	relBase := filepath.Clean(uploadRoot)
	for _, rel := range relativeImagePaths {
		abs := filepath.Join(relBase, filepath.FromSlash(rel))
		data, err := os.ReadFile(abs)
		if err != nil {
			return "", fmt.Errorf("read image %s: %w", rel, err)
		}
		head := data
		if len(head) > 512 {
			head = head[:512]
		}
		mime := http.DetectContentType(head)
		if !strings.HasPrefix(mime, "image/") {
			return "", fmt.Errorf("not an image file: %s", rel)
		}
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(payload))
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
		return "", fmt.Errorf("openai vision http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
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
func GPTSinglePassSkinCoach(ctx context.Context, cfg *config.Config, httpClient *http.Client, uploadRoot string, relativeImagePaths []string, userContextAndSchema string, skillLevel string) (string, error) {
	if cfg == nil || strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return "", fmt.Errorf("openai: missing api key")
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
	relBase := filepath.Clean(uploadRoot)
	for _, rel := range relativeImagePaths {
		abs := filepath.Join(relBase, filepath.FromSlash(rel))
		data, err := os.ReadFile(abs)
		if err != nil {
			return "", fmt.Errorf("read image %s: %w", rel, err)
		}
		// Empty / non-image bytes shouldn't reach OpenAI — vision call would either
		// 400 or (worse) return an empty assistant message that breaks JSON parse later.
		if len(data) == 0 {
			return "", fmt.Errorf("image %s is empty (0 bytes)", rel)
		}
		head := data
		if len(head) > 512 {
			head = head[:512]
		}
		mime := http.DetectContentType(head)
		if !strings.HasPrefix(mime, "image/") {
			return "", fmt.Errorf("file %s is not a valid image (mime=%s)", rel, mime)
		}
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(payload))
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
		return "", fmt.Errorf("openai coach http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
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
