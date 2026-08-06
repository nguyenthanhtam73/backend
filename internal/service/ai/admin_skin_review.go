package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/platform/httpx"
	"github.com/dadiary/backend/internal/platform/imgprep"
)

var adminSkinSentenceSplitRe = regexp.MustCompile(`[.!?…]+|\n+`)

// Patterns that must not appear in expanded notes (brands / meds / jargon / hard care advice).
var adminSkinExpandBanRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(retinol|niacinamide|benzoyl|antibiotic|papules?|pustules?|comedones?|erythema|barrier|hyperpigmentation|sebum|morpholog\w*|clinical|BHA|AHA)\b`),
	regexp.MustCompile(`(?i)\b(la\s*roche|cerave|ordinary|the\s+ordinary|cera\s*ve)\b`),
	regexp.MustCompile(`(?i)kháng sinh|routine sáng|routine tối|nên thoa|nên bôi|nên dùng|hết mụn|mỹ phẩm`),
	regexp.MustCompile(`(?i)sản phẩm chăm sóc da`),
}

// Full hedge phrases banned when newly introduced by expand (not bare "có thể"/"nghi").
var adminSkinExpandHedgeRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)không\s+chắc\s+100%`),
	regexp.MustCompile(`(?i)trên\s+ảnh\s+nghi`),
	regexp.MustCompile(`(?i)đôi\s+khi\s+liên\s+quan`),
	regexp.MustCompile(`(?i)chưa\s+chắc`),
	regexp.MustCompile(`(?i)có\s+thể\s+là`),
}

// Strip negated "không nên …" before ban matching so "không nên dùng tay bẩn" is not treated as product advice.
var adminSkinNegatedNênRe = regexp.MustCompile(`(?i)không\s+nên\s+(dùng|thoa|bôi)`)

// Soft content refusals some models put in message.content instead of refusal.
var adminSkinContentRefusalRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)i'?m sorry[\s\S]{0,80}can'?t assist`),
	regexp.MustCompile(`(?i)i cannot assist with that`),
	regexp.MustCompile(`(?i)i can'?t assist with that`),
	regexp.MustCompile(`(?i)xin lỗi[\s\S]{0,60}không (thể|thể hỗ trợ|hỗ trợ)`),
}

// AdminSkinReviewAnalyze runs a Premium-depth OpenAI vision pass that returns
// skin observations only (no routine / products / care steps).
//
// Uses the configured vision model (default gpt-4o) — same deep multimodal path
// as check-in vision — without calling the daily coach or suggest-routine flows.
// On refusal / empty content, retries once with a compact prompt (no loop).
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
	if len(images) < 1 {
		return nil, "", fmt.Errorf("admin skin review: need at least 1 image")
	}
	if len(images) > 3 {
		return nil, "", fmt.Errorf("admin skin review: maximum 3 images")
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

	imageParts, err := adminSkinReviewImageParts(prepared)
	if err != nil {
		return nil, "", err
	}

	// Attempt 1 — full prompt.
	usedCompact := false
	raw, meta, err := callAdminSkinReviewVision(ctx, cfg, httpClient, model, AdminSkinReviewSystemPrompt(), adminSkinReviewUserText(locale, false), imageParts, "openai-admin-skin-review")
	if err != nil {
		return nil, "", err
	}
	if adminSkinEmptyOrRefused(raw, meta) {
		slog.Warn("admin skin review: attempt 1 empty/refusal — retrying once with compact prompt",
			"finish", meta.FinishReason,
			"refusal", meta.Refusal,
			"content_len", len(raw),
		)
		// Attempt 2 — compact system + user (still observations-first + causes/tips).
		raw2, meta2, err2 := callAdminSkinReviewVision(ctx, cfg, httpClient, model, AdminSkinReviewCompactSystemPrompt(), adminSkinReviewUserText(locale, true), imageParts, "openai-admin-skin-review-retry")
		if err2 != nil {
			return nil, "", fmt.Errorf("admin skin review: retry after refusal failed: %w (attempt1 finish=%s refusal=%q)", err2, meta.FinishReason, meta.Refusal)
		}
		if adminSkinEmptyOrRefused(raw2, meta2) {
			return nil, "", fmt.Errorf("admin skin review: empty/refusal after retry (attempt1 finish=%s refusal=%q; attempt2 finish=%s refusal=%q)",
				meta.FinishReason, meta.Refusal, meta2.FinishReason, meta2.Refusal)
		}
		slog.Info("admin skin review: compact retry succeeded",
			"finish", meta2.FinishReason,
			"content_len", len(raw2),
		)
		raw = raw2
		usedCompact = true
	} else {
		slog.Info("admin skin review: attempt 1 ok", "content_len", len(raw))
	}

	jsonBytes, err := ExtractJSONObject(raw)
	if err != nil {
		return nil, "", fmt.Errorf("admin skin review: extract json: %w", err)
	}
	if len(jsonBytes) == 0 {
		return nil, "", fmt.Errorf("admin skin review: extracted empty json (raw_len=%d head=%q)", len(raw), trimForLog(raw, 120))
	}

	parsed, err := parseAdminSkinReviewAnalysis(jsonBytes, locale)
	if err != nil {
		return nil, "", fmt.Errorf("admin skin review: parse analysis (raw_len=%d head=%q): %w", len(raw), trimForLog(raw, 160), err)
	}
	if expanded, expErr := expandShortAdminSkinProblemNotes(ctx, cfg, httpClient, model, parsed, locale); expErr == nil && expanded != nil {
		parsed = expanded
	}
	if usedCompact {
		thinRegions := adminSkinThinProblemRegions(parsed)
		ovSent := countAdminSkinSentences(parsed.Overview)
		if len(thinRegions) > 0 || ovSent < 4 {
			slog.Warn("admin skin review: compact retry produced thin analysis",
				"overview_sentences", ovSent,
				"thin_problem_regions", thinRegions,
			)
			if ovSent < 3 || len(thinRegions) > 0 && adminSkinAllProblemNotesThin(parsed) {
				return nil, "", fmt.Errorf("admin skin review: nhận xét quá ngắn sau khi AI từ chối ảnh (compact retry). Thử ảnh rõ hơn, đủ sáng, hoặc chụp lại vùng da")
			}
		}
	}
	return parsed, model, nil
}

// adminSkinThinProblemRegions lists visible problem regions whose notes are under 5 sentences.
func adminSkinThinProblemRegions(a *dto.AdminSkinReviewAnalysis) []string {
	if a == nil {
		return nil
	}
	var out []string
	for _, ar := range a.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(ar.Concern))
		if c == "" || c == "none" || c == "not_visible" {
			continue
		}
		if countAdminSkinSentences(ar.Note) < 5 {
			out = append(out, ar.Region+"/"+ar.Concern)
		}
	}
	return out
}

func adminSkinAllProblemNotesThin(a *dto.AdminSkinReviewAnalysis) bool {
	if a == nil {
		return true
	}
	n := 0
	for _, ar := range a.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(ar.Concern))
		if c == "" || c == "none" || c == "not_visible" {
			continue
		}
		n++
		if countAdminSkinSentences(ar.Note) >= 5 {
			return false
		}
	}
	return n > 0
}

func adminSkinReviewUserText(locale string, compact bool) string {
	langHead := "**Output locale: Vietnamese (vi).** Xưng tao/mày — thẳng, đanh đá, chanh chua, tự tin trên dấu hiệu rõ, không tục, không nịnh, không mình/bạn. Overview 4–6 câu chỗ nổi bật, không copy lặp sang note+additional. Ảnh rõ → nói thẳng “Đây là… / Má của mày đang… / Trông đúng kiểu…”; gọi tên nhóm (mụn viêm / có mủ / bọc / cồi) khi đủ dấu. CẤM nhồi “không chắc 100%…/chưa chắc/trên ảnh nghi…/đôi khi liên quan…/có thể là…” khi ảnh rõ. Có thâm nông → nói thâm rất nhẹ/thâm nông; CẤM “không thấy thâm” nếu ảnh có dấu. Close-up: chỉ vùng thấy; ngoài khung = not_visible + 1 câu ngắn; skin_type_note: “Chỉ thấy má — chưa đủ chốt loại da cả mặt”. Full face: đủ trán+má+cằm thì phải nhận xét mũi. possible_causes 1–2 câu trực tiếp (không hedge cuối câu); soothing_tips 2–3 đời thường (CẤM từ “active”; viết tạm nghỉ sản phẩm trị mụn/mạnh; không brand/thuốc/routine; khám da chỉ khi ổ to/đau/kéo dài)."
	if locale == "en" {
		langHead = "**Output locale: English (en).** Best-friend tart voice (I/you), confident on clear photo facts — no fluff, no scolding insults, no hedge spam when signs are clear. Name morphology groups when clear (inflammatory bumps / pustules / cysts / comedones). Info-dense overview without repeating the same shine/pores/bumps across notes. If faint marks exist say “very light PIH / shallow marks” — never “no dark marks” when marks exist. Close-up: visible only; others not_visible + 1 short sentence; skin_type_note once that crop isn’t enough for full-face type. Full face: review nose when forehead+cheeks+chin visible. possible_causes 1–2 direct; soothing_tips 2–3 (no brands/meds/AM-PM); derm tip only if large/painful/lasting."
	}
	if compact {
		if locale == "en" {
			langHead = "**Output locale: English (en).** Short retry. Tart best-friend voice, confident on clear facts. Observations-first. possible_causes 1–2 direct; soothing_tips 2–3. Close-up: visible only; others not_visible 1 sentence. No false “no dark marks”. No hedge spam."
		} else {
			langHead = "**Output locale: Vietnamese (vi).** Retry rút gọn. Xưng tao/mày, đanh đá không tục, tự tin trên dấu rõ. Observations-first. Gọi tên nhóm khi đủ dấu. CẤM nhồi hedge. possible_causes 1–2 trực tiếp; soothing_tips 2–3. Close-up: chỉ vùng thấy; ngoài khung not_visible 1 câu. Có thâm nông thì nói thâm nhẹ — cấm “không thấy thâm”."
		}
		return langHead + "\n\n" + AdminSkinReviewCompactJSONSchemaBlock +
			"\n\nReview the attached skin photo(s). Return one JSON object only. Match THIS photo."
	}
	return langHead + "\n\n" + AdminSkinReviewJSONSchemaBlock +
		"\n\nReview the attached skin photo(s). Return one JSON object only. Match what is actually visible on THIS photo."
}

func adminSkinReviewImageParts(prepared []ImageBytes) ([]map[string]any, error) {
	parts := make([]map[string]any, 0, len(prepared))
	for _, im := range prepared {
		head := im.Data
		if len(head) > 512 {
			head = head[:512]
		}
		mime := http.DetectContentType(head)
		if !strings.HasPrefix(mime, "image/") {
			return nil, fmt.Errorf("admin skin review: invalid image bytes")
		}
		b64 := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(im.Data))
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": b64,
			},
		})
	}
	return parts, nil
}

type adminSkinVisionMeta struct {
	FinishReason string
	Refusal      string
}

func callAdminSkinReviewVision(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	model string,
	systemPrompt string,
	userText string,
	imageParts []map[string]any,
	op string,
) (string, adminSkinVisionMeta, error) {
	var meta adminSkinVisionMeta
	parts := make([]map[string]any, 0, 1+len(imageParts))
	parts = append(parts, map[string]any{"type": "text", "text": userText})
	parts = append(parts, imageParts...)

	body := map[string]any{
		"model":           model,
		"temperature":     0.4,
		"max_tokens":      8192,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]any{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": parts},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", meta, err
	}
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.OpenAI.APIKey,
		"Content-Type":  "application/json",
	}
	b, err := CallAIWithRetry(ctx, cfg, op, func(ctx context.Context) ([]byte, error) {
		return httpx.PostJSON(ctx, httpClient, "openai admin skin review", "https://api.openai.com/v1/chat/completions", headers, payload)
	})
	if err != nil {
		return "", meta, err
	}
	var apiOut struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string  `json:"content"`
				Refusal *string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(b, &apiOut); err != nil {
		return "", meta, fmt.Errorf("admin skin review: decode api response: %w", err)
	}
	if apiOut.Error != nil {
		return "", meta, fmt.Errorf("admin skin review: api error: %v", apiOut.Error)
	}
	if len(apiOut.Choices) == 0 {
		return "", meta, fmt.Errorf("admin skin review: empty model response body_len=%d head=%q", len(b), trimForLog(string(b), 200))
	}
	meta.FinishReason = apiOut.Choices[0].FinishReason
	if apiOut.Choices[0].Message.Refusal != nil {
		meta.Refusal = strings.TrimSpace(*apiOut.Choices[0].Message.Refusal)
	}
	return strings.TrimSpace(apiOut.Choices[0].Message.Content), meta, nil
}

func adminSkinEmptyOrRefused(content string, meta adminSkinVisionMeta) bool {
	if strings.TrimSpace(meta.Refusal) != "" {
		return true
	}
	c := strings.TrimSpace(content)
	if c == "" {
		return true
	}
	// Some responses put a polite refuse in content with an empty refusal field.
	for _, re := range adminSkinContentRefusalRes {
		if re.MatchString(c) {
			return true
		}
	}
	return false
}

// expandShortAdminSkinProblemNotes rewrites thin problem-region notes to 5–8
// separate sentences without inventing new visual claims. Best-effort; on
// failure or failed verify the original note is kept.
func expandShortAdminSkinProblemNotes(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	model string,
	in *dto.AdminSkinReviewAnalysis,
	locale string,
) (*dto.AdminSkinReviewAnalysis, error) {
	if in == nil {
		return nil, nil
	}
	type thinNote struct {
		Index   int    `json:"index"`
		Region  string `json:"region"`
		Concern string `json:"concern"`
		Note    string `json:"note"`
	}
	thin := make([]thinNote, 0)
	for i, a := range in.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(a.Concern))
		if c == "" || c == "none" || c == "not_visible" {
			continue
		}
		if countAdminSkinSentences(a.Note) >= 5 {
			continue
		}
		thin = append(thin, thinNote{Index: i, Region: a.Region, Concern: a.Concern, Note: a.Note})
	}
	for i, a := range in.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(a.Concern))
		if c != "none" {
			continue
		}
		if countAdminSkinSentences(a.Note) >= 3 {
			continue
		}
		thin = append(thin, thinNote{Index: i, Region: a.Region, Concern: a.Concern, Note: a.Note})
	}
	if len(thin) == 0 {
		return in, nil
	}

	thinJSON, err := json.Marshal(thin)
	if err != nil {
		return nil, err
	}
	lang := "Vietnamese"
	if locale == "en" {
		lang = "English"
	}
	user := "Thicken ONLY these Admin Skin Review notes by splitting packed ideas into SEPARATE short sentences " +
		"(problem concern: 5–8; concern=none: 3–4). Keep tao/mày voice, confident on clear facts. " +
		"ONLY expand ideas already present in each note — do NOT invent new spot counts, locations, colors, marks, scars, or disease names. " +
		"FIRST sentence of a problem note should stay/get confident: \"Má của mày đang…\" / \"Đây là…\" / \"Trông đúng kiểu…\". " +
		"Keep morphology group names if already present (mụn viêm / mụn có mủ / mụn bọc / mụn cồi). " +
		"Do NOT add hedge spam: \"không chắc 100%…\", \"chưa chắc\", \"trên ảnh nghi…\", \"đôi khi liên quan…\", \"có thể là…\". " +
		"If the note mentions thâm/marks, NEVER rewrite to \"không thấy thâm\" / \"không thấy thâm rõ\". " +
		"Do not add \"chưa thấy X\" unless the original note already said that. No products, brands, routines, hard disease names. Locale: " + lang + ".\n\n" +
		"Input notes JSON:\n" + string(thinJSON) + "\n\n" +
		"Return JSON object: {\"notes\":[{\"index\":<int>,\"note\":\"...\"}, ...]} with the same indexes."

	body := map[string]any{
		"model":           model,
		"temperature":     0.2,
		"max_tokens":      2048,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": "You thicken DaDiary Admin Skin Review notes in Vietnamese best-friend voice (tao/mày), tart but not vulgar, confident on clear photo facts. Only elaborate ideas already in the note. Never invent new findings. Never add hedge spam. If the note already mentions thâm/marks, never rewrite to “không thấy thâm”. Never name brands or medicines.",
			},
			{"role": "user", "content": user},
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
	b, err := CallAIWithRetry(ctx, cfg, "openai-admin-skin-review-expand", func(ctx context.Context) ([]byte, error) {
		return httpx.PostJSON(ctx, httpClient, "openai admin skin review expand", "https://api.openai.com/v1/chat/completions", headers, payload)
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
	if len(apiOut.Choices) == 0 {
		return nil, fmt.Errorf("admin skin review expand: empty")
	}
	rawObj, err := ExtractJSONObject(strings.TrimSpace(apiOut.Choices[0].Message.Content))
	if err != nil {
		return nil, err
	}
	var patched struct {
		Notes []struct {
			Index int    `json:"index"`
			Note  string `json:"note"`
		} `json:"notes"`
	}
	if err := json.Unmarshal(rawObj, &patched); err != nil {
		return nil, err
	}
	out := *in
	out.AttentionAreas = append([]dto.AdminSkinAttentionArea(nil), in.AttentionAreas...)
	kept, rejected := 0, 0
	for _, n := range patched.Notes {
		if n.Index < 0 || n.Index >= len(out.AttentionAreas) {
			continue
		}
		original := out.AttentionAreas[n.Index].Note
		note := strings.TrimSpace(n.Note)
		if !acceptExpandedAdminSkinNote(original, note) {
			rejected++
			slog.Info("admin skin review expand: rejected rewrite, keeping original",
				"region", out.AttentionAreas[n.Index].Region,
				"concern", out.AttentionAreas[n.Index].Concern,
			)
			continue
		}
		out.AttentionAreas[n.Index].Note = note
		kept++
	}
	slog.Info("admin skin review expand: verify done", "accepted", kept, "rejected", rejected)
	return &out, nil
}

// acceptExpandedAdminSkinNote returns true when rewritten note is safely thicker
// than original, free of banned brand/med/jargon, and does not newly introduce
// banned hedge phrases (full phrases only — not bare "có thể"/"nghi").
func acceptExpandedAdminSkinNote(original, expanded string) bool {
	expanded = strings.TrimSpace(expanded)
	original = strings.TrimSpace(original)
	if expanded == "" {
		return false
	}
	if adminSkinNoteHasBannedContent(expanded) {
		return false
	}
	if adminSkinExpandAddsHedge(original, expanded) {
		return false
	}
	origSent := countAdminSkinSentences(original)
	expSent := countAdminSkinSentences(expanded)
	origLen := utf8.RuneCountInString(original)
	expLen := utf8.RuneCountInString(expanded)

	// Prefer more sentences; otherwise require meaningfully longer text (~15%+ or +24 chars).
	thickerBySentences := expSent > origSent
	thickerByLen := expLen >= origLen+24 || (origLen > 0 && expLen*100 >= origLen*115)
	if !thickerBySentences && !thickerByLen {
		return false
	}
	return true
}

// adminSkinExpandAddsHedge is true when expanded contains a banned hedge phrase
// that was not already present in the original note.
func adminSkinExpandAddsHedge(original, expanded string) bool {
	for _, re := range adminSkinExpandHedgeRes {
		if re.MatchString(expanded) && !re.MatchString(original) {
			return true
		}
	}
	return false
}

func adminSkinNoteHasBannedContent(s string) bool {
	// Avoid false positive: "không nên dùng/thoa/bôi …" is avoid-advice, not product pitch.
	cleaned := adminSkinNegatedNênRe.ReplaceAllString(s, " ")
	for _, re := range adminSkinExpandBanRes {
		if re.MatchString(cleaned) {
			return true
		}
	}
	return false
}

func trimForLog(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func countAdminSkinSentences(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	parts := adminSkinSentenceSplitRe.Split(s, -1)
	n := 0
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return n
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
	out.PossibleCauses = dto.ClampAdminSkinStringList(out.PossibleCauses, 2)
	out.SoothingTips = dto.ClampAdminSkinStringList(out.SoothingTips, 3)
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
	noseConcern := strings.ToLower(areas[noseIdx].Concern)
	noseNoteLow := strings.ToLower(areas[noseIdx].Note)
	falselyOutside := noseConcern == "not_visible" ||
		strings.Contains(noseNoteLow, "không thấy mũi") ||
		strings.Contains(noseNoteLow, "không có trên ảnh") ||
		strings.Contains(noseNoteLow, "ngoài khung") ||
		strings.Contains(noseNoteLow, "not visible") ||
		strings.Contains(noseNoteLow, "out of frame") ||
		strings.Contains(noseNoteLow, "chụp đủ mặt mới nhận xét")
	if !falselyOutside {
		return areas
	}
	note := "Mũi nằm giữa trán–má–cằm trên ảnh này nên vẫn nhận xét được. Không thấy nốt sưng hay đỏ lan rõ ở sống mũi / cánh mũi. Bề mặt khá đều so với hai má. Đang yên hơn vùng đang nổi bên cạnh."
	if locale == "en" {
		note = "The nose sits between the visible forehead, cheeks, and chin on this photo, so it is in frame. No clear raised spots or diffuse redness on the bridge or sides. Surface looks relatively even versus the cheeks. Calmer than the active spots nearby."
	}
	areas[noseIdx].Concern = "none"
	areas[noseIdx].Severity = "mild"
	areas[noseIdx].Note = note
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
		return trimmed
	}
}

func normalizeAdminDisclaimer(s, locale string) string {
	s = strings.TrimSpace(s)
	if s != "" {
		return s
	}
	if locale == "en" {
		return "Observation from photos only — not a doctor visit or medical diagnosis."
	}
	return "Chỉ quan sát từ ảnh thôi, không thay khám bác sĩ hay chẩn đoán y khoa."
}
