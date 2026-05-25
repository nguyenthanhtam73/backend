package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
)

func onboardingOutputLocale(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "en":
		return "en"
	default:
		return "vi"
	}
}

// ImageBytes is one decoded onboarding photo (JPEG/PNG/WebP/GIF).
type ImageBytes struct {
	Data []byte
}

// OnboardingSkinAnalyze runs GPT-4 class vision on 2–3 **facial** skin images and returns structured guesses.
// Uses OpenAI Vision model only (DADIARY_OPENAI_VISION_MODEL or fallback to DADIARY_OPENAI_MODEL / gpt-4o).
// localeRaw should be "vi" or "en" (app UI); it controls coaching_notes / disclaimers / tips language.
func OnboardingSkinAnalyze(ctx context.Context, cfg *config.Config, httpClient *http.Client, images []ImageBytes, localeRaw string) (*dto.OnboardingSkinAnalyzeResponse, error) {
	if cfg == nil || strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return nil, fmt.Errorf("onboarding skin: openai api key required")
	}
	locale := onboardingOutputLocale(localeRaw)
	if len(images) < 2 || len(images) > 3 {
		return nil, fmt.Errorf("onboarding skin: need 2 to 3 images")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	model := strings.TrimSpace(cfg.OpenAI.VisionModel)
	if model == "" {
		model = strings.TrimSpace(cfg.OpenAI.Model)
	}
	if model == "" {
		model = "gpt-4o"
	}

	langHead := "**Output locale: Vietnamese (vi).** Write coaching_notes, non_diagnostic, and photo_quality.tips only in natural Vietnamese."
	if locale == "en" {
		langHead = "**Output locale: English (en).** Write coaching_notes, non_diagnostic, and photo_quality.tips only in natural English."
	}
	userText := langHead + "\n\n" + OnboardingSkinJSONSchemaBlock + "\n\nPhotos: **2–3 close, well-lit photos of facial skin** (natural light, little or no makeup). Include a **front** view plus a **slight side/profile** if possible—this flow is optimized for face-only onboarding."
	parts := []map[string]any{
		{"type": "text", "text": userText},
	}
	for _, im := range images {
		if len(im.Data) == 0 {
			continue
		}
		head := im.Data
		if len(head) > 512 {
			head = head[:512]
		}
		mime := http.DetectContentType(head)
		if !strings.HasPrefix(mime, "image/") {
			return nil, fmt.Errorf("onboarding skin: invalid image bytes")
		}
		b64 := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(im.Data))
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": b64,
			},
		})
	}
	if len(parts) < 3 {
		return nil, fmt.Errorf("onboarding skin: not enough valid images")
	}

	body := map[string]any{
		"model":           model,
		"temperature":     0.25,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": OnboardingSkinVisionPrompt(),
			},
			{
				"role":    "user",
				"content": parts,
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAI.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai onboarding vision http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var apiOut struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &apiOut); err != nil {
		return nil, err
	}
	if len(apiOut.Choices) == 0 || strings.TrimSpace(apiOut.Choices[0].Message.Content) == "" {
		return nil, fmt.Errorf("openai onboarding: empty response")
	}
	raw, err := ExtractJSONObject(apiOut.Choices[0].Message.Content)
	if err != nil {
		return nil, err
	}
	var parsed dto.OnboardingSkinAnalyzeResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse onboarding json: %w", err)
	}
	parsed.NonDiagnostic = normalizeOnboardingDisclaimer(parsed.NonDiagnostic, locale)
	parsed.ModelUsed = model
	return &parsed, nil
}
