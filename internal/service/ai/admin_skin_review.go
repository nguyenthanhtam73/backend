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

	langHead := "**Output locale: Vietnamese (vi).** Warm, lightly witty, never mocking. Write LONG, information-dense notes — short answers FAIL. Length floors (sentences ending . ! ? …): overview 4–6; skin_type_note exactly 2; each attention note 3–5 (none regions still 3–4 explaining WHY calm); additional_observations 3–5; photo_notes 2–3. Scan forehead→nose→cheeks→chin; mark concern \"none\" ONLY if truly clear. Raised spots/heads/clusters → acne|papules|pustules (not redness-only). Problem notes must cover location detail, count/density, color, morphology, contrast vs nearby zones, and how obvious on photo. Never mention products, brands, mỹ phẩm, or care advice."
	if locale == "en" {
		langHead = "**Output locale: English (en).** Warm, lightly witty, never mocking. Write LONG, information-dense notes — short answers FAIL. Length floors: overview 4–6 sentences; skin_type_note exactly 2; each attention note 3–5 (none regions still 3–4 explaining WHY calm); additional_observations 3–5; photo_notes 2–3. Scan forehead→nose→cheeks→chin; mark concern \"none\" ONLY if truly clear. Raised spots/heads/clusters → acne|papules|pustules (not redness-only). Problem notes must cover location detail, count/density, color, morphology, contrast vs nearby zones, and how obvious on photo. Never mention products, brands, or care advice."
	}
	userText := langHead + "\n\n" + AdminSkinReviewJSONSchemaBlock +
		"\n\nPhotos: **1–3 skin photos** for deep observation-only review.\n" +
		"Do not miss visible spots on forehead/chin/cheeks — if spots exist, concern must not be \"none\".\n" +
		"Prefer acne/papules/pustules when raised lesions are visible; use redness only for diffuse flush.\n" +
		"LENGTH: do not write short filler. Hit the sentence floors above; pack visual detail into every note.\n" +
		"Do NOT invent routines, products, brands, care steps, treatment advice, or medical diagnoses.\n" +
		"Banned: \"sản phẩm chăm sóc da\", \"mỹ phẩm\", \"nên dùng\", \"nên thoa\", \"nên bôi\"."

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
		"temperature":     0.5,  // warm / lightly witty voice; still observation-grounded
		"max_tokens":      8192, // room for verbose multi-region visual notes
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
	out.SkinTypeSeverity = normalizeAdminSeverityLevel(out.SkinTypeSeverity)
	if out.SkinTypeSeverity == "" {
		out.SkinTypeSeverity = normalizeAdminSeverityLevel(out.OverallSeverity)
	}
	if out.SkinTypeSeverity == "" {
		out.SkinTypeSeverity = "mild"
	}
	out.SkinTypeNote = strings.TrimSpace(out.SkinTypeNote)
	out.AdditionalObservations = strings.TrimSpace(out.AdditionalObservations)
	if out.AdditionalObservations == "" {
		parts := make([]string, 0, 2)
		if s := strings.TrimSpace(out.DetailedFindings); s != "" {
			parts = append(parts, s)
		}
		if s := strings.TrimSpace(out.ExtraNotes); s != "" {
			parts = append(parts, s)
		}
		out.AdditionalObservations = strings.Join(parts, "\n\n")
	}
	out.PhotoNotes = strings.TrimSpace(out.PhotoNotes)
	if out.PhotoNotes == "" {
		if q := strings.TrimSpace(out.PhotoQuality); q != "" && !strings.EqualFold(q, "good") {
			out.PhotoNotes = fallbackPhotoNotes(q, locale)
		} else {
			out.PhotoNotes = defaultClearPhotoNotes(locale)
		}
	}
	out.NonDiagnostic = normalizeAdminDisclaimer(out.NonDiagnostic, locale)

	areas := make([]dto.AdminSkinAttentionArea, 0, len(out.AttentionAreas))
	for _, a := range out.AttentionAreas {
		region := normalizeAdminRegion(a.Region)
		note := strings.TrimSpace(a.Note)
		concern := normalizeAdminConcern(a.Concern)
		severity := normalizeAdminSeverityLevel(a.Severity)
		if severity == "" {
			severity = "mild"
		}
		// Drop empty rows (no region + no note + generic concern).
		if region == "other" && note == "" && concern == "other" {
			continue
		}
		areas = append(areas, dto.AdminSkinAttentionArea{
			Region:   region,
			Concern:  concern,
			Severity: severity,
			Note:     note,
		})
	}
	out.AttentionAreas = areas

	// Canonical response shape only.
	out.OverallSeverity = ""
	out.ExtraNotes = ""
	out.DetailedFindings = ""
	out.PhotoQuality = ""

	return &out, nil
}

func defaultClearPhotoNotes(locale string) string {
	if locale == "en" {
		return "Photos are clear enough for review"
	}
	return "Ảnh đủ rõ để nhận xét"
}

func fallbackPhotoNotes(quality, locale string) string {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "poor":
		if locale == "en" {
			return "Photo quality is limited (lighting, angle, or blur) — review may be incomplete."
		}
		return "Ảnh chưa rõ (ánh sáng, góc, hoặc bị mờ) — nhận xét có thể chưa đủ."
	case "average":
		if locale == "en" {
			return "Photo quality is average — some cues may be harder to judge."
		}
		return "Chất lượng ảnh trung bình — một số dấu hiệu có thể khó đánh giá."
	default:
		return defaultClearPhotoNotes(locale)
	}
}

func normalizeAdminSkinType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "oily", "dry", "combination", "normal", "sensitive", "unclear":
		return strings.ToLower(strings.TrimSpace(v))
	case "combo":
		return "combination"
	default:
		return "unclear"
	}
}

// normalizeAdminSeverityLevel returns mild|moderate|pronounced (no "clear").
func normalizeAdminSeverityLevel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "mild", "moderate", "pronounced":
		return strings.ToLower(strings.TrimSpace(v))
	case "severe":
		return "pronounced"
	case "clear", "none", "low":
		return "mild"
	default:
		return ""
	}
}

func normalizeAdminConcern(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "none", "clear", "ok", "normal":
		return "none"
	case "papule":
		return "papules"
	case "pustule":
		return "pustules"
	case "pigment", "pih", "hyperpigmentation":
		return "pigmentation"
	case "acne", "papules", "pustules", "redness", "pigmentation", "dark_spots",
		"pores", "dryness", "oiliness", "texture", "irritation", "other":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "other"
	}
}

func normalizeAdminRegion(v string) string {
	trimmed := strings.TrimSpace(v)
	key := strings.ToLower(trimmed)
	key = strings.ReplaceAll(key, " ", "_")
	key = strings.ReplaceAll(key, "-", "_")
	switch key {
	case "forehead", "trán", "tran":
		return "forehead"
	case "cheeks", "cheek", "má", "ma":
		return "cheeks"
	case "nose", "mũi", "mui":
		return "nose"
	case "chin", "cằm", "cam":
		return "chin"
	case "t_zone", "tzone", "t":
		return "t_zone"
	case "jawline", "jaw", "hàm", "ham":
		return "jawline"
	case "under_eyes", "undereyes", "under_eye":
		return "under_eyes"
	case "other":
		return "other"
	default:
		if trimmed == "" {
			return "other"
		}
		// Preserve free-text region labels for display when model doesn't use enums.
		return trimmed
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
