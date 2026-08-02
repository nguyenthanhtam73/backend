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
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/platform/httpx"
	"github.com/dadiary/backend/internal/platform/imgprep"
)

var wardrobeLabelCategories = map[string]struct{}{
	"cleanser": {}, "toner": {}, "serum": {}, "moisturizer": {},
	"spf": {}, "treatment": {}, "mask": {}, "other": {},
}

type wardrobeLabelScanRaw struct {
	Name       string  `json:"name"`
	Brand      string  `json:"brand"`
	Category   string  `json:"category"`
	Notes      string  `json:"notes"`
	Confidence float64 `json:"confidence"`
}

// WardrobeLabelScan runs GPT vision OCR on a single product-label photo.
// Does not persist anything — caller returns suggestions for the client to confirm.
func WardrobeLabelScan(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	image []byte,
	localeRaw string,
) (*dto.WardrobeLabelScanResponse, error) {
	if cfg == nil || strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return nil, fmt.Errorf("wardrobe label scan: openai api key required")
	}
	if len(image) == 0 {
		return nil, fmt.Errorf("wardrobe label scan: empty image")
	}
	prepared, err := imgprep.LimitForVisionAPI(image)
	if err != nil {
		return nil, fmt.Errorf("wardrobe label scan: prepare image: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	model := cfg.OpenAIVisionModel()
	locale := onboardingOutputLocale(localeRaw)

	langHead := "**Output locale for notes: Vietnamese (vi).** Keep name/brand as printed on the pack."
	if locale == "en" {
		langHead = "**Output locale for notes: English (en).** Keep name/brand as printed on the pack."
	}
	userText := langHead + "\n\n" + WardrobeLabelScanJSONSchemaBlock +
		"\n\nPhoto: **one clear product label** (front of pack / bottle). Read printed text only."

	head := prepared
	if len(head) > 512 {
		head = head[:512]
	}
	mime := http.DetectContentType(head)
	if !strings.HasPrefix(mime, "image/") {
		return nil, fmt.Errorf("wardrobe label scan: invalid image bytes")
	}
	b64 := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(prepared))
	parts := []map[string]any{
		{"type": "text", "text": userText},
		{
			"type": "image_url",
			"image_url": map[string]any{
				"url": b64,
			},
		},
	}

	body := map[string]any{
		"model":           model,
		"temperature":     0.1,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": WardrobeLabelScanSystemPrompt(),
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
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.OpenAI.APIKey,
		"Content-Type":  "application/json",
	}
	b, err := CallAIWithRetry(ctx, cfg, "openai-wardrobe-label", func(ctx context.Context) ([]byte, error) {
		return httpx.PostJSON(ctx, httpClient, "openai wardrobe label", "https://api.openai.com/v1/chat/completions", headers, payload)
	})
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("openai wardrobe label: empty response")
	}
	raw, err := ExtractJSONObject(apiOut.Choices[0].Message.Content)
	if err != nil {
		return nil, err
	}
	var parsed wardrobeLabelScanRaw
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse wardrobe label json: %w", err)
	}
	out := normalizeWardrobeLabelScan(parsed)
	return &out, nil
}

func normalizeWardrobeLabelScan(raw wardrobeLabelScanRaw) dto.WardrobeLabelScanResponse {
	cat := strings.ToLower(strings.TrimSpace(raw.Category))
	if _, ok := wardrobeLabelCategories[cat]; !ok {
		cat = "other"
	}
	conf := raw.Confidence
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}
	return dto.WardrobeLabelScanResponse{
		Name:       strings.TrimSpace(raw.Name),
		Brand:      strings.TrimSpace(raw.Brand),
		Category:   cat,
		Notes:      strings.TrimSpace(raw.Notes),
		Confidence: conf,
	}
}
