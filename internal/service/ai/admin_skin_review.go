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

	langHead := "**Output locale: Vietnamese (vi).** Best-friend voice: straight, lightly tart / mild scolding when spots are clear — never mocking, never xàm, never forced cool. Write for non-experts: every technical idea in overview/notes must be everyday Vietnamese — nốt đỏ sưng, nốt có đầu trắng, thâm, da bóng, da khô, lỗ chân lông to, da hơi sần… Enum keys (papules/pustules/texture/mild/not_visible…) OK only in concern/severity/skin_type fields — NEVER paste those English/medical terms into overview, notes, additional_observations, photo_notes, or skin_type_note. BAN sến phrases: ồn ào, party, drama, lên tiếng, bận rộn, chill, ngồi yên. FRAME LOCALIZATION: top-of-frame/narrow upper strip → forehead (NOT chin unless lower-face landmarks); bottom → chin; center → nose/cheeks. Narrow crop → photo_notes say “ảnh crop chỉ một dải…”, one primary visible region, others concern=not_visible. NOSE: frontal/¾ with bridge/alae → must review nose (none or real concern); not_visible ONLY if truly cut/covered — never fake outside. VISIBLE-ONLY: off-frame → concern \"not_visible\" + EXACTLY 1 short sentence (\"Không thấy X trên ảnh — chụp đủ mặt mới nhận xét được.\"); BAN filler. SINGLE-REGION crop (e.g. forehead only): visible note 4–6 THICK sentences (density/spread, color/swelling/heads, oil/dry, precise location); overview 3–5 stuck to visible zone + missing-face reminder. Never invent calm; never use \"none\" for missing regions. Length: full-face overview 4–6; single-region overview 3–5; skin_type_note 2; single-region problem ≥4; full-face problem 3–5; visible calm 3–4; not_visible = 1; additional 3–5; photo_notes 2–3. Scan forehead→nose→cheeks→chin always. Raised spots/heads/clusters → concern acne|papules|pustules (not redness-only) but describe as nốt đỏ sưng / nốt có đầu trắng / mụn in the note text. Problem notes must cover location detail, count/density, color, shape (sưng/phẳng/đầu trắng), contrast vs nearby VISIBLE zones, how obvious on photo, optional mild accountability beat. Never mention products, brands, mỹ phẩm, or care advice."
	if locale == "en" {
		langHead = "**Output locale: English (en).** Best-friend voice: straight, lightly tart / mild scolding when spots are clear — never mocking, never try-hard cool. Write for non-experts: plain everyday words in overview/notes (red bumps, whiteheads, dark marks, shiny, dry, large pores, uneven surface) — do NOT dump clinical jargon (papules, pustules, erythema, barrier, hyperpigmentation, sebum, morphology) into user-facing text. Enum keys OK only in concern/severity/skin_type fields. BAN cute filler like \"drama\", \"party\", \"noisy\". FRAME LOCALIZATION: top-of-frame/narrow upper strip → forehead (NOT chin unless lower-face landmarks); bottom → chin; center → nose/cheeks. Narrow crop → photo_notes say it is a strip crop, one primary visible region, others concern=not_visible. NOSE: frontal/¾ with bridge/alae → must review nose; not_visible ONLY if truly cut/covered. VISIBLE-ONLY: off-frame → concern \"not_visible\" + EXACTLY 1 short sentence. SINGLE-REGION crop: visible note 4–6 thick sentences; overview 3–5. Never invent calm. Length: full-face overview 4–6; single-region overview 3–5; skin_type_note 2; single-region problem ≥4; full-face problem 3–5; visible calm 3–4; not_visible = 1; additional 3–5; photo_notes 2–3. Scan forehead→nose→cheeks→chin always. Raised spots/heads/clusters → concern acne|papules|pustules (not redness-only); note text stays plain English. Problem notes must cover location detail, count/density, color, shape, contrast vs nearby VISIBLE zones, and how obvious on photo. Never mention products, brands, or care advice."
	}
	userText := langHead + "\n\n" + AdminSkinReviewJSONSchemaBlock +
		"\n\nPhotos: **1–3 skin photos** for deep observation-only review.\n" +
		"FRAME: narrow strip without lips/mouth → forehead (NOT chin). Chin only if lips/mouth/jaw visible. photo_notes \"ảnh crop chỉ một dải…\" + one primary region; others not_visible.\n" +
		"NOSE: if forehead+cheeks+chin are all visible on this photo, nose must NOT be not_visible — use none or a real concern. Same if other notes mention sống mũi/cánh mũi.\n" +
		"Do not miss visible spots on forehead/chin/cheeks — if spots exist on a VISIBLE region, concern must not be \"none\" or \"not_visible\".\n" +
		"If a region is off-frame / not visible: concern \"not_visible\" + EXACTLY 1 sentence: \"Không thấy X trên ảnh — chụp đủ mặt mới nhận xét được.\" BAN filler. NEVER invent calm/bad skin; do not use concern \"none\" for missing regions.\n" +
		"SINGLE-REGION crop (only forehead/cheek/chin visible): that region's note MUST be 4–6 thick sentences — density/spread, color/swelling/whiteheads, oil/dry, precise location. overview 3–5 sentences on the visible zone + short missing-face reminder.\n" +
		"Do NOT copy few-shot wording — count/location/color/shine must match THIS photo.\n" +
		"Prefer concern acne/papules/pustules when raised spots are visible; use redness only for diffuse flush. In note text say nốt đỏ sưng / nốt có đầu trắng / mụn (VI) or red bumps / whiteheads (EN) — never leave bare papules/pustules/not_visible in notes.\n" +
		"PLAIN LANGUAGE: user must not need a dictionary. Ban in overview/notes: papules, pustules, comedone, erythema, barrier, inflammation, hyperpigmentation, sebum, texture, morphology, clinical, buccal, PIH, T-zone (say vùng chữ T: trán–mũi–cằm).\n" +
		"BAN sến: ồn ào, party, drama, lên tiếng, bận rộn.\n" +
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
		"temperature":     0.45, // straight / lightly tart friend voice; still observation-grounded
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
	out.AttentionAreas = coerceNoseIfFaceCenterPresent(out.AttentionAreas, locale)

	// Canonical response shape only.
	out.OverallSeverity = ""
	out.ExtraNotes = ""
	out.DetailedFindings = ""
	out.PhotoQuality = ""

	return &out, nil
}

// coerceNoseIfFaceCenterPresent fixes a recurring model error: marking nose
// not_visible while forehead + cheeks + chin are all reviewed on the same photo.
func coerceNoseIfFaceCenterPresent(areas []dto.AdminSkinAttentionArea, locale string) []dto.AdminSkinAttentionArea {
	visible := map[string]bool{}
	noseIdx := -1
	for i, a := range areas {
		r := strings.ToLower(a.Region)
		c := strings.ToLower(a.Concern)
		if r == "nose" {
			noseIdx = i
		}
		if c != "not_visible" && (r == "forehead" || r == "cheeks" || r == "chin") {
			visible[r] = true
		}
	}
	if noseIdx < 0 || !visible["forehead"] || !visible["cheeks"] || !visible["chin"] {
		return areas
	}
	if strings.ToLower(areas[noseIdx].Concern) != "not_visible" {
		return areas
	}
	note := "Mũi nằm giữa trán–má–cằm trên ảnh này nên vẫn nhận xét được. Không thấy nốt sưng hay đỏ lan rõ ở sống mũi / cánh mũi. Bề mặt khá đều so với hai má. Đang yên hơn vùng đang nổi bên cạnh."
	if locale == "en" {
		note = "The nose sits between the visible forehead, cheeks, and chin on this photo, so it is in frame. No clear raised spots or diffuse redness on the bridge or sides. Surface looks relatively even versus the cheeks. Calmer than the active spots nearby."
	}
	areas[noseIdx].Concern = "none"
	areas[noseIdx].Severity = "mild"
	if strings.TrimSpace(areas[noseIdx].Note) == "" || strings.Contains(strings.ToLower(areas[noseIdx].Note), "ngoài khung") ||
		strings.Contains(strings.ToLower(areas[noseIdx].Note), "không có trên ảnh") ||
		strings.Contains(strings.ToLower(areas[noseIdx].Note), "not visible") ||
		strings.Contains(strings.ToLower(areas[noseIdx].Note), "out of frame") {
		areas[noseIdx].Note = note
	}
	return areas
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
	case "not_visible", "notvisible", "off_frame", "offframe", "out_of_frame", "unavailable":
		return "not_visible"
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
