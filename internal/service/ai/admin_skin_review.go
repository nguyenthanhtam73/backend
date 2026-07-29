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

// AdminSkinReviewAnalyze runs a Premium-depth OpenAI vision pass that returns
// skin observations only (no routine / products / care steps).
//
// Uses the configured vision model (default gpt-4o) — same deep multimodal path
// as check-in vision — without calling the daily coach or suggest-routine flows.
func AdminSkinReviewAnalyze(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	images []ImageBytes,
	localeRaw string,
) (*dto.AdminSkinReviewAnalysis, string, error) {
	if cfg == nil || strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return nil, "", fmt.Errorf("admin skin review: openai api key required")
	}
	if len(images) < 1 || len(images) > 3 {
		return nil, "", fmt.Errorf("admin skin review: need 1 to 3 images")
	}
	locale := dto.NormalizeAdminSkinReviewLocale(localeRaw)

	prepared := make([]ImageBytes, 0, len(images))
	for i, im := range images {
		data, err := imgprep.LimitForVisionAPI(im.Data)
		if err != nil {
			return nil, "", fmt.Errorf("admin skin review: prepare image %d: %w", i+1, err)
		}
		if len(data) == 0 {
			continue
		}
		prepared = append(prepared, ImageBytes{Data: data})
	}
	if len(prepared) < 1 {
		return nil, "", fmt.Errorf("admin skin review: no valid images")
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	model := cfg.OpenAIVisionModel()
	logVisionModelSelection("admin-skin-review", model)

	langHead := "**Output locale: Vietnamese (vi).** Write overview, attention_areas[].note, extra_notes, detailed_findings, and non_diagnostic in natural Vietnamese."
	if locale == "en" {
		langHead = "**Output locale: English (en).** Write overview, attention_areas[].note, extra_notes, detailed_findings, and non_diagnostic in natural English."
	}
	userText := langHead + "\n\n" + AdminSkinReviewJSONSchemaBlock +
		"\n\nPhotos: **1–3 well-lit skin photos** for deep observation-only review. " +
		"Describe what is visible. Do NOT invent routines, products, or care steps."

	parts := []map[string]any{
		{"type": "text", "text": userText},
	}
	for _, im := range prepared {
		head := im.Data
		if len(head) > 512 {
			head = head[:512]
		}
		mime := http.DetectContentType(head)
		if !strings.HasPrefix(mime, "image/") {
			return nil, "", fmt.Errorf("admin skin review: invalid image bytes")
		}
		b64 := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(im.Data))
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
				"content": AdminSkinReviewSystemPrompt(),
			},
			{
				"role":    "user",
				"content": parts,
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.OpenAI.APIKey,
		"Content-Type":  "application/json",
	}

	b, err := CallAIWithRetry(ctx, cfg, "openai-admin-skin-review", func(ctx context.Context) ([]byte, error) {
		return httpx.PostJSON(ctx, httpClient, "openai admin skin review", "https://api.openai.com/v1/chat/completions", headers, payload)
	})
	if err != nil {
		return nil, "", err
	}

	var apiOut struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &apiOut); err != nil {
		return nil, "", fmt.Errorf("admin skin review: decode api response: %w", err)
	}
	if len(apiOut.Choices) == 0 {
		return nil, "", fmt.Errorf("admin skin review: empty model response")
	}
	raw := strings.TrimSpace(apiOut.Choices[0].Message.Content)
	jsonBytes, err := ExtractJSONObject(raw)
	if err != nil {
		return nil, "", fmt.Errorf("admin skin review: extract json: %w", err)
	}

	parsed, err := parseAdminSkinReviewAnalysis(jsonBytes, locale)
	if err != nil {
		return nil, "", err
	}
	return parsed, model, nil
}

func parseAdminSkinReviewAnalysis(raw []byte, locale string) (*dto.AdminSkinReviewAnalysis, error) {
	var out dto.AdminSkinReviewAnalysis
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("admin skin review: parse analysis: %w", err)
	}
	out.Overview = strings.TrimSpace(out.Overview)
	out.SkinType = normalizeAdminSkinType(out.SkinType)
	out.OverallSeverity = normalizeAdminSeverity(out.OverallSeverity)
	out.ExtraNotes = strings.TrimSpace(out.ExtraNotes)
	out.DetailedFindings = strings.TrimSpace(out.DetailedFindings)
	out.PhotoQuality = normalizeAdminPhotoQuality(out.PhotoQuality)
	out.NonDiagnostic = normalizeAdminDisclaimer(out.NonDiagnostic, locale)

	areas := make([]dto.AdminSkinAttentionArea, 0, len(out.AttentionAreas))
	for _, a := range out.AttentionAreas {
		region := strings.TrimSpace(a.Region)
		note := strings.TrimSpace(a.Note)
		if region == "" && note == "" {
			continue
		}
		areas = append(areas, dto.AdminSkinAttentionArea{
			Region:   region,
			Concern:  normalizeAdminConcern(a.Concern),
			Severity: normalizeAdminSeverity(a.Severity),
			Note:     note,
		})
	}
	out.AttentionAreas = areas
	return &out, nil
}

func normalizeAdminSkinType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "oily", "dry", "combination", "combo", "normal", "sensitive", "unclear":
		if strings.EqualFold(strings.TrimSpace(v), "combo") {
			return "combination"
		}
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "unclear"
	}
}

func normalizeAdminSeverity(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "clear", "mild", "moderate", "pronounced", "severe":
		if strings.EqualFold(strings.TrimSpace(v), "severe") {
			return "pronounced"
		}
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "mild"
	}
}

func normalizeAdminConcern(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "acne", "dark_spots", "redness", "pores", "texture", "dryness", "oiliness", "other":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "other"
	}
}

func normalizeAdminPhotoQuality(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "good", "average", "poor":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "average"
	}
}

func normalizeAdminDisclaimer(s, locale string) string {
	s = strings.TrimSpace(s)
	if s != "" {
		return s
	}
	if locale == "en" {
		return "Observation from photos only — not a medical diagnosis."
	}
	return "Chỉ là nhận xét từ ảnh, không phải chẩn đoán y khoa."
}
