// Temporary smoke runner — not part of the product. Safe to delete.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/platform/httpx"
	"github.com/dadiary/backend/internal/platform/imgprep"
	"github.com/dadiary/backend/internal/service/ai"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: smoke <image.jpg> [locale]")
		os.Exit(2)
	}
	imgPath := os.Args[1]
	locale := "vi"
	if len(os.Args) > 2 {
		locale = os.Args[2]
	}

	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if !cfg.HasOpenAIKey() {
		fmt.Fprintln(os.Stderr, "missing DADIARY_OPENAI_API_KEY")
		os.Exit(1)
	}

	raw, err := os.ReadFile(imgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read image: %v\n", err)
		os.Exit(1)
	}
	data, err := imgprep.LimitForVisionAPI(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prep image: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// Prefer product path first.
	analysis, model, err := ai.AdminSkinReviewAnalyze(
		ctx,
		cfg,
		&http.Client{Timeout: 4 * time.Minute},
		[]ai.ImageBytes{{Data: raw}},
		locale,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "AdminSkinReviewAnalyze error: %v\n", err)
		fmt.Fprintln(os.Stderr, "--- dumping raw OpenAI chat completion for diagnosis ---")
		dumpRaw, dumpErr := callOpenAIRaw(ctx, cfg, data, locale)
		if dumpErr != nil {
			fmt.Fprintf(os.Stderr, "raw dump also failed: %v\n", dumpErr)
			os.Exit(1)
		}
		fmt.Println(dumpRaw)
		os.Exit(1)
	}

	type envelope struct {
		Source    string                       `json:"source"`
		ModelUsed string                       `json:"model_used"`
		Locale    string                       `json:"locale"`
		Analysis  *dto.AdminSkinReviewAnalysis `json:"analysis"`
	}
	out := envelope{
		Source:    "AdminSkinReviewAnalyze (local code + railway env; equivalent to POST /api/v1/admin/skin-review AI core)",
		ModelUsed: model,
		Locale:    locale,
		Analysis:  analysis,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println("=== FULL JSON RESPONSE ===")
	fmt.Println(string(b))

	fmt.Println("\n=== FIELD LENGTHS (chars / sentences) ===")
	printLen("overview", analysis.Overview)
	printLen("skin_type", analysis.SkinType)
	printLen("skin_type_severity", analysis.SkinTypeSeverity)
	printLen("skin_type_note", analysis.SkinTypeNote)
	for i, a := range analysis.AttentionAreas {
		prefix := fmt.Sprintf("attention_areas[%d]", i)
		printLen(prefix+".region", a.Region)
		printLen(prefix+".concern", a.Concern)
		printLen(prefix+".severity", a.Severity)
		printLen(prefix+".note", a.Note)
	}
	printLen("additional_observations", analysis.AdditionalObservations)
	printLen("photo_notes", analysis.PhotoNotes)
	printLen("non_diagnostic", analysis.NonDiagnostic)

	fmt.Println("\n=== LENGTH FLOOR CHECKLIST ===")
	fail := false
	fail = checkFloor("overview", analysis.Overview, 4) || fail
	fail = checkFloor("skin_type_note", analysis.SkinTypeNote, 2) || fail
	for i, a := range analysis.AttentionAreas {
		min := 3
		label := fmt.Sprintf("attention_areas[%d].note (%s/%s)", i, a.Region, a.Concern)
		if a.Concern != "none" {
			min = 3 // problem notes ≥3 (target 3–5)
		}
		fail = checkFloor(label, a.Note, min) || fail
	}
	fail = checkFloor("additional_observations", analysis.AdditionalObservations, 3) || fail
	fail = checkFloor("photo_notes", analysis.PhotoNotes, 2) || fail
	if fail {
		fmt.Println("LENGTH CHECK: FAIL")
	} else {
		fmt.Println("LENGTH CHECK: PASS")
	}

	fmt.Println("\n=== COVERAGE / VISUAL CHECKLIST ===")
	regions := map[string]bool{}
	for _, a := range analysis.AttentionAreas {
		regions[strings.ToLower(a.Region)] = true
	}
	fmt.Printf("attention_areas count: %d\n", len(analysis.AttentionAreas))
	for _, req := range []string{"forehead", "nose", "cheeks", "chin"} {
		if regions[req] {
			fmt.Printf("region %s: PRESENT\n", req)
		} else {
			fmt.Printf("region %s: MISSING\n", req)
		}
	}
	genericRe := regexp.MustCompile(`không đều nhẹ|ánh sáng phản chiếu|có thể do thời tiết`)
	colorRe := regexp.MustCompile(`(?i)hồng|đỏ|nâu|vàng|trắng|sáng|tối|bóng|khô|dầu|lỗ chân lông|thâm|giữa trán|cánh mũi|hai má|cằm|trán|mũi|má`)
	locRe := regexp.MustCompile(`(?i)trán|má|mũi|cằm|cánh mũi|giữa trán|hai má|vùng T|t-zone|under`)
	shineRe := regexp.MustCompile(`(?i)bóng|khô|dầu|shine|dry|oil`)
	for i, a := range analysis.AttentionAreas {
		note := a.Note
		hasColorOrShine := colorRe.MatchString(note) || shineRe.MatchString(note)
		hasLoc := locRe.MatchString(note)
		generic := genericRe.FindString(note)
		fmt.Printf("note[%d] region=%s concern=%s visual(color/shine)=%v location=%v generic=%q sentences=%d\n",
			i, a.Region, a.Concern, hasColorOrShine, hasLoc, generic, countSentences(note))
	}

	fmt.Println("\n=== ROUTINE/PRODUCT BAN CHECK ===")
	hits := banHits(analysis)
	if len(hits) == 0 {
		fmt.Println("NO VIOLATION detected (no routine/product/care-step keywords found).")
	} else {
		fmt.Println("POSSIBLE VIOLATIONS:")
		for _, h := range hits {
			fmt.Println("-", h)
		}
	}

	fmt.Println("\n=== PLAIN-LANGUAGE / JARGON CHECK (user-facing strings only) ===")
	jargon := jargonHits(analysis)
	if len(jargon) == 0 {
		fmt.Println("NO jargon detected in overview/notes — readable without a dictionary.")
	} else {
		fmt.Println("JARGON HITS (user would need to look up):")
		for _, h := range jargon {
			fmt.Println("-", h)
		}
	}

	if fail || len(hits) > 0 || len(jargon) > 0 {
		os.Exit(1)
	}
}

func checkFloor(name, s string, min int) bool {
	n := countSentences(s)
	ok := n >= min
	status := "OK"
	if !ok {
		status = "FAIL"
	}
	fmt.Printf("[%s] %s: %d sentences (min %d)\n", status, name, n, min)
	return !ok
}

func callOpenAIRaw(ctx context.Context, cfg *config.Config, data []byte, locale string) (string, error) {
	model := cfg.OpenAIVisionModel()
	langHead := "**Output locale: Vietnamese (vi).**"
	if locale == "en" {
		langHead = "**Output locale: English (en).**"
	}
	mime := http.DetectContentType(data)
	b64 := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data))
	body := map[string]any{
		"model":           model,
		"temperature":     0.45,
		"max_tokens":      4096,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]any{
			{"role": "system", "content": ai.AdminSkinReviewSystemPrompt()},
			{"role": "user", "content": []map[string]any{
				{"type": "text", "text": langHead + "\n\n" + ai.AdminSkinReviewJSONSchemaBlock + "\n\nPhotos for observation-only review."},
				{"type": "image_url", "image_url": map[string]any{"url": b64}},
			}},
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
	client := &http.Client{Timeout: 4 * time.Minute}
	b, err := httpx.PostJSON(ctx, client, "openai admin skin review dump", "https://api.openai.com/v1/chat/completions", headers, payload)
	if err != nil {
		return "", err
	}
	// Redact nothing needed; response has no API key. Pretty-print a slim view.
	var apiOut map[string]any
	_ = json.Unmarshal(b, &apiOut)
	// Keep content + finish_reason only.
	slim := map[string]any{}
	if id, ok := apiOut["id"]; ok {
		slim["id"] = id
	}
	if m, ok := apiOut["model"]; ok {
		slim["model"] = m
	}
	if ch, ok := apiOut["choices"].([]any); ok && len(ch) > 0 {
		slim["choices"] = ch
	}
	if errObj, ok := apiOut["error"]; ok {
		slim["error"] = errObj
	}
	out, _ := json.MarshalIndent(slim, "", "  ")
	return string(out), nil
}

func printLen(name, s string) {
	s = strings.TrimSpace(s)
	fmt.Printf("%s: %d chars / %d sentences\n", name, utf8.RuneCountInString(s), countSentences(s))
}

func countSentences(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	re := regexp.MustCompile(`[.!?…]+|\n+`)
	parts := re.Split(s, -1)
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

// jargonHits scans user-facing text only (not concern/severity enum fields).
func jargonHits(a *dto.AdminSkinReviewAnalysis) []string {
	// Word-ish English/medical terms that must not appear in displayed notes.
	patterns := []string{
		`(?i)\bpapules?\b`, `(?i)\bpustules?\b`, `(?i)\bcomedones?\b`, `(?i)\berythema\b`,
		`(?i)\bbarrier\b`, `(?i)\binflammat\w*\b`, `(?i)\bhyperpigmentation\b`, `(?i)\bsebum\b`,
		`(?i)\btexture\b`, `(?i)\bmorpholog\w*\b`, `(?i)\blesions?\b`, `(?i)\bclinical\b`,
		`(?i)\bbuccal\b`, `(?i)\bPIH\b`, `(?i)\bT-?zone\b`, `(?i)\bseverity\b`,
		`(?i)\bacne\b`, `(?i)\bpores?\b`, `(?i)\boiliness\b`, `(?i)\bdryness\b`,
		`(?i)\birritation\b`, `(?i)\bpigmentation\b`,
		`hàng rào da`, `mức moderate`, `mức mild`, `mức pronounced`,
	}
	type field struct {
		name string
		text string
	}
	fields := []field{
		{"overview", a.Overview},
		{"skin_type_note", a.SkinTypeNote},
		{"additional_observations", a.AdditionalObservations},
		{"photo_notes", a.PhotoNotes},
		{"non_diagnostic", a.NonDiagnostic},
	}
	for i, area := range a.AttentionAreas {
		fields = append(fields, field{fmt.Sprintf("attention_areas[%d].note", i), area.Note})
	}
	var hits []string
	seen := map[string]bool{}
	for _, f := range fields {
		for _, p := range patterns {
			re := regexp.MustCompile(p)
			if m := re.FindString(f.text); m != "" {
				key := f.name + "|" + strings.ToLower(m)
				if seen[key] {
					continue
				}
				seen[key] = true
				hits = append(hits, fmt.Sprintf("%s contains %q", f.name, m))
			}
		}
	}
	return hits
}

func banHits(a *dto.AdminSkinReviewAnalysis) []string {
	patterns := []string{
		`(?i)\broutine\b`, `(?i)\bproduct(s)?\b`, `(?i)\bserum\b`, `(?i)\bcleanser\b`,
		`(?i)\bmoisturizer\b`, `(?i)\bretinol\b`, `(?i)\bniacinamide\b`, `(?i)\bsunscreen\b`,
		`(?i)\bspf\b`, `(?i)\btreatment\b`, `(?i)\bapply\b`, `(?i)\buse\s+\w+\s+(cream|gel|oil)`,
		`(?i)\brecommend(ed|ation)?\b`, `(?i)\bshould\s+use\b`, `(?i)\btry\s+using\b`,
		`(?i)\bingredient`, `(?i)\bskincare\s+step`,
		`routine`, `sản phẩm chăm sóc da`, `sản phẩm`, `mỹ phẩm`, `serum`, `cleanser`,
		`sữa rửa mặt`, `kem dưỡng`, `kem chống nắng`, `retinol`, `niacinamide`, `toner`,
		`exfoliat`, `nên dùng`, `nên thoa`, `nên bôi`, `hãy dùng`, `áp dụng`,
		`bước chăm sóc`, `chăm sóc da`, `điều trị`, `treatment`, `AHA`, `BHA`, `vitamin C`,
	}
	type field struct {
		name string
		text string
	}
	fields := []field{
		{"overview", a.Overview},
		{"skin_type", a.SkinType},
		{"skin_type_severity", a.SkinTypeSeverity},
		{"skin_type_note", a.SkinTypeNote},
		{"additional_observations", a.AdditionalObservations},
		{"photo_notes", a.PhotoNotes},
		{"non_diagnostic", a.NonDiagnostic},
	}
	for i, area := range a.AttentionAreas {
		fields = append(fields,
			field{fmt.Sprintf("attention_areas[%d].region", i), area.Region},
			field{fmt.Sprintf("attention_areas[%d].concern", i), area.Concern},
			field{fmt.Sprintf("attention_areas[%d].severity", i), area.Severity},
			field{fmt.Sprintf("attention_areas[%d].note", i), area.Note},
		)
	}
	var hits []string
	for _, f := range fields {
		for _, p := range patterns {
			re := regexp.MustCompile(p)
			if m := re.FindString(f.text); m != "" {
				hits = append(hits, fmt.Sprintf("%s contains %q (pattern %s)", f.name, m, p))
			}
		}
	}
	return hits
}
