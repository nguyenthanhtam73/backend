// Temporary runner: Admin Skin Review voice + visible-only checklist.
// Safe to delete. Usage (from backend/):
//
//	go run ./tmp_test_assets/visible_tone/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/service/ai"
)

type checkRow struct {
	Case   string
	ID     string
	Status string // PASS | FAIL
	Detail string
}

func main() {
	assetsDir := filepath.Join("tmp_test_assets")
	if _, err := os.Stat(assetsDir); err != nil {
		// Allow running from repo root.
		assetsDir = filepath.Join("backend", "tmp_test_assets")
	}
	foreheadPath := filepath.Join(assetsDir, "forehead_only.jpg")
	spotsPath := filepath.Join(assetsDir, "face_with_spots.jpg")
	sourceAcne := filepath.Join(assetsDir, "acne_clear_issues.jpg")

	if err := ensureAssets(sourceAcne, foreheadPath, spotsPath); err != nil {
		fmt.Fprintf(os.Stderr, "prepare assets: %v\n", err)
		os.Exit(1)
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

	client := &http.Client{Timeout: 5 * time.Minute}
	locale := "vi"
	var rows []checkRow
	overallFail := false

	cases := []struct {
		name string
		path string
		fn   func(*dto.AdminSkinReviewAnalysis) []checkRow
	}{
		{"forehead_only", foreheadPath, checkForeheadOnly},
		{"face_with_spots", spotsPath, checkFaceWithSpots},
	}
	only := ""
	if len(os.Args) > 1 {
		only = strings.TrimSpace(os.Args[1])
	}

	for _, c := range cases {
		if only != "" && c.name != only {
			continue
		}
		fmt.Printf("\n######## CASE: %s (%s) ########\n", c.name, c.path)
		raw, err := os.ReadFile(c.path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", c.path, err)
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		analysis, model, err := ai.AdminSkinReviewAnalyze(
			ctx, cfg, client, []ai.ImageBytes{{Data: raw}}, locale,
		)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "AdminSkinReviewAnalyze(%s): %v\n", c.name, err)
			os.Exit(1)
		}

		env := map[string]any{
			"case":       c.name,
			"source":     "AdminSkinReviewAnalyze (local production prompt)",
			"model_used": model,
			"locale":     locale,
			"image":      c.path,
			"analysis":   analysis,
		}
		b, _ := json.MarshalIndent(env, "", "  ")
		fmt.Println("=== RAW JSON ===")
		fmt.Println(string(b))

		caseRows := c.fn(analysis)
		caseRows = append(caseRows, checkShared(c.name, analysis)...)
		for _, r := range caseRows {
			if r.Status == "FAIL" {
				overallFail = true
			}
			rows = append(rows, r)
		}
	}

	fmt.Println("\n======== CHECKLIST PASS/FAIL ========")
	fmt.Printf("| %-16s | %-42s | %-4s | %s\n", "CASE", "CHECK", "OK?", "DETAIL")
	fmt.Println("|" + strings.Repeat("-", 18) + "|" + strings.Repeat("-", 44) + "|" + strings.Repeat("-", 6) + "|" + strings.Repeat("-", 40))
	for _, r := range rows {
		fmt.Printf("| %-16s | %-42s | %-4s | %s\n", r.Case, r.ID, r.Status, compact(r.Detail, 80))
	}

	fails := 0
	for _, r := range rows {
		if r.Status == "FAIL" {
			fails++
		}
	}
	fmt.Printf("\nSUMMARY: %d checks, %d FAIL\n", len(rows), fails)
	if overallFail {
		fmt.Println("\n=== PROMPT DIFF SUGGESTIONS (do not apply unless asked) ===")
		printPromptSuggestions(rows)
		os.Exit(1)
	}
	fmt.Println("OVERALL: PASS")
}

func ensureAssets(sourceAcne, foreheadPath, spotsPath string) error {
	if _, err := os.Stat(sourceAcne); err != nil {
		return fmt.Errorf("missing source %s (run gen_acne or restore assets)", sourceAcne)
	}
	// Always refresh face_with_spots with a painted nose bridge so full-face
	// checks are fair on the synthetic acne canvas (otherwise vision invents
	// "nose outside frame" even when forehead+cheeks+chin are all scored).
	if err := writeFaceWithNose(sourceAcne, spotsPath); err != nil {
		return err
	}
	fmt.Printf("wrote %s (acne + painted nose bridge)\n", spotsPath)
	if _, err := os.Stat(foreheadPath); err == nil {
		fmt.Printf("using existing %s\n", foreheadPath)
		return nil
	}
	if err := cropForeheadOnly(sourceAcne, foreheadPath); err != nil {
		return err
	}
	fmt.Printf("wrote %s (top ~32%% crop of %s)\n", foreheadPath, filepath.Base(sourceAcne))
	return nil
}

func writeFaceWithNose(srcPath, dstPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		return err
	}
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	// Simple vertical nose bridge + small alae in the face center.
	cx := (b.Min.X + b.Max.X) / 2
	y0 := b.Min.Y + b.Dy()*38/100
	y1 := b.Min.Y + b.Dy()*58/100
	for y := y0; y < y1; y++ {
		for dx := -10; dx <= 10; dx++ {
			x := cx + dx
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			shade := uint8(150 + absInt(dx)*4)
			rgba.Set(x, y, color.RGBA{shade, shade - 30, shade - 45, 255})
		}
	}
	// Alae bulbs
	for _, ox := range []int{-22, 22} {
		for dy := -12; dy <= 12; dy++ {
			for dx := -14; dx <= 14; dx++ {
				if dx*dx+dy*dy > 160 {
					continue
				}
				x, y := cx+ox+dx, y1-8+dy
				if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
					continue
				}
				rgba.Set(x, y, color.RGBA{175, 120, 100, 255})
			}
		}
	}
	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()
	return jpeg.Encode(out, rgba, &jpeg.Options{Quality: 92})
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func cropForeheadOnly(srcPath, dstPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		return err
	}
	b := img.Bounds()
	// Top ~32% — forehead band on synthetic 800x1000 face; excludes cheeks/chin.
	h := b.Dy() * 32 / 100
	if h < 80 {
		h = b.Dy() / 3
	}
	rect := image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Min.Y+h)
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	var cropped image.Image
	if si, ok := img.(subImager); ok {
		cropped = si.SubImage(rect)
	} else {
		return fmt.Errorf("image type %T does not support SubImage", img)
	}
	// Paint a dark hairline band at the top edge so strip reads as upper face / forehead
	// (synthetic skin alone is easy to mislabel as chin).
	rgba := image.NewRGBA(cropped.Bounds())
	for y := cropped.Bounds().Min.Y; y < cropped.Bounds().Max.Y; y++ {
		for x := cropped.Bounds().Min.X; x < cropped.Bounds().Max.X; x++ {
			rgba.Set(x, y, cropped.At(x, y))
		}
	}
	hairH := rgba.Bounds().Dy() / 8
	if hairH < 12 {
		hairH = 12
	}
	for y := rgba.Bounds().Min.Y; y < rgba.Bounds().Min.Y+hairH; y++ {
		for x := rgba.Bounds().Min.X; x < rgba.Bounds().Max.X; x++ {
			rgba.Set(x, y, color.RGBA{10, 8, 6, 255})
		}
	}
	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()
	return jpeg.Encode(out, rgba, &jpeg.Options{Quality: 92})
}

func checkShared(caseName string, a *dto.AdminSkinReviewAnalysis) []checkRow {
	rows := []checkRow{
		row(caseName, "C.has_overview", strings.TrimSpace(a.Overview) != "", "overview empty"),
		row(caseName, "C.has_non_diagnostic", strings.TrimSpace(a.NonDiagnostic) != "", "non_diagnostic empty"),
		row(caseName, "C.no_routine_product", len(banHits(a)) == 0, joinHits(banHits(a))),
	}
	return rows
}

func checkForeheadOnly(a *dto.AdminSkinReviewAnalysis) []checkRow {
	offRegions := []string{"nose", "cheeks", "chin"}
	var rows []checkRow

	// Soft issue #1: primary visible band must be forehead, not chin.
	fh := findRegion(a, "forehead")
	chin := findRegion(a, "chin")
	primaryOK := fh != nil && !strings.EqualFold(fh.Concern, "not_visible")
	chinOff := chin != nil && (strings.EqualFold(chin.Concern, "not_visible") || looksNotVisible(strings.ToLower(chin.Note)))
	chinNotPrimary := true
	if chin != nil && !strings.EqualFold(chin.Concern, "not_visible") && !looksNotVisible(strings.ToLower(chin.Note)) {
		// Chin treated as visible/primary while this asset is a top-band crop.
		chinNotPrimary = false
	}
	rows = append(rows, row("forehead_only", "A1.primary_forehead_not_chin",
		primaryOK && chinOff && chinNotPrimary,
		fmt.Sprintf("forehead=%v concern=%q; chin=%v concern=%q",
			fh != nil, concernOf(fh), chin != nil, concernOf(chin))))

	fakeCalm := false
	var fakeDetails []string
	missingNotVisible := false
	var missDetails []string
	tooLong := false
	var longDetails []string
	fillerHit := false
	var fillerDetails []string
	fillerRe := regexp.MustCompile(`(?i)không bịa|đoán mò|không có cơ sở|ngoài khung thì mình`)

	fmt.Println("\n=== not_visible NOTES (nose / cheeks / chin) ===")
	for _, region := range offRegions {
		area := findRegion(a, region)
		if area == nil {
			missingNotVisible = true
			missDetails = append(missDetails, region+": MISSING from attention_areas")
			fmt.Printf("%s: MISSING\n", region)
			continue
		}
		note := area.Note
		noteLow := strings.ToLower(note)
		nSent := countSentences(note)
		fmt.Printf("%s [%s] (%d câu): %s\n", region, area.Concern, nSent, note)
		if looksLikeFakeCalm(noteLow) {
			fakeCalm = true
			fakeDetails = append(fakeDetails, fmt.Sprintf("%s note invents visible calm: %q", region, compact(note, 60)))
		}
		if !looksNotVisible(noteLow) && !strings.EqualFold(area.Concern, "not_visible") {
			missingNotVisible = true
			missDetails = append(missDetails, fmt.Sprintf("%s expected not_visible / no-info cue, got concern=%q note=%q", region, area.Concern, compact(note, 60)))
		}
		if nSent > 2 {
			tooLong = true
			longDetails = append(longDetails, fmt.Sprintf("%s=%d câu: %q", region, nSent, compact(note, 50)))
		}
		if fillerRe.MatchString(note) {
			fillerHit = true
			fillerDetails = append(fillerDetails, fmt.Sprintf("%s: %q", region, compact(note, 50)))
		}
	}
	rows = append(rows,
		row("forehead_only", "A.offframe_not_fake_calm", !fakeCalm, strings.Join(fakeDetails, "; ")),
		row("forehead_only", "A.offframe_no_info_cue", !missingNotVisible, strings.Join(missDetails, "; ")),
		row("forehead_only", "A.offframe_note_short", !tooLong, strings.Join(longDetails, "; ")),
		row("forehead_only", "A.offframe_no_filler", !fillerHit, strings.Join(fillerDetails, "; ")),
	)

	pn := strings.ToLower(a.PhotoNotes)
	photoOK := strings.Contains(pn, "trán") || strings.Contains(pn, "forehead") ||
		strings.Contains(pn, "chỉ thấy") || strings.Contains(pn, "crop") ||
		strings.Contains(pn, "một dải") || strings.Contains(pn, "ngoài khung") || strings.Contains(pn, "thiếu") ||
		(strings.Contains(pn, "phần") && (strings.Contains(pn, "thấy") || strings.Contains(pn, "hiện")))
	// Must not claim the visible band is chin.
	photoWrongChin := strings.Contains(pn, "chỉ thấy") && strings.Contains(pn, "cằm") && !strings.Contains(pn, "trán")
	rows = append(rows, row("forehead_only", "A.photo_notes_visibility", photoOK && !photoWrongChin,
		"photo_notes should state forehead / strip crop (not chin-only): "+compact(a.PhotoNotes, 70)))
	return rows
}

func checkFaceWithSpots(a *dto.AdminSkinReviewAnalysis) []checkRow {
	var rows []checkRow

	// Soft issue #2: nose must be reviewed on full-face; not falsely not_visible/out of frame.
	nose := findRegion(a, "nose")
	noseOK := false
	noseDetail := "nose missing from attention_areas"
	if nose != nil {
		c := strings.ToLower(strings.TrimSpace(nose.Concern))
		noteLow := strings.ToLower(nose.Note)
		falselyOutside := c == "not_visible" || looksNotVisible(noteLow)
		noseOK = !falselyOutside
		noseDetail = fmt.Sprintf("concern=%q note=%q", nose.Concern, compact(nose.Note, 70))
		if falselyOutside {
			noseDetail = "nose falsely outside/not_visible on full face: " + noseDetail
		}
	}
	rows = append(rows, row("face_with_spots", "B2.nose_present_not_outside", noseOK, noseDetail))

	problemOK := false
	var problemDetail string
	for _, area := range a.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(area.Concern))
		if c == "" || c == "none" || c == "not_visible" {
			continue
		}
		note := area.Note
		if hasConcreteSpotDetail(note) {
			problemOK = true
			problemDetail = fmt.Sprintf("%s/%s: %s", area.Region, area.Concern, compact(note, 70))
			break
		}
		problemDetail = fmt.Sprintf("%s/%s lacks count/color/location: %s", area.Region, area.Concern, compact(note, 70))
	}
	if !problemOK && problemDetail == "" {
		problemDetail = "no attention_area with concern != none/not_visible"
	}
	rows = append(rows, row("face_with_spots", "B.problem_region_concrete", problemOK, problemDetail))

	senHits := senPhraseHits(a)
	rows = append(rows, row("face_with_spots", "B.no_sen_phrases", len(senHits) == 0, joinHits(senHits)))

	jargon := jargonHits(a)
	rows = append(rows, row("face_with_spots", "B.no_heavy_jargon", len(jargon) == 0, joinHits(jargon)))
	return rows
}

func concernOf(a *dto.AdminSkinAttentionArea) string {
	if a == nil {
		return ""
	}
	return a.Concern
}

func row(caseName, id string, pass bool, detail string) checkRow {
	st := "PASS"
	if !pass {
		st = "FAIL"
		if strings.TrimSpace(detail) == "" {
			detail = "failed"
		}
	} else {
		// On PASS, keep informative detail only when it isn't a failure message template.
		if strings.TrimSpace(detail) == "" ||
			strings.HasSuffix(detail, " empty") ||
			detail == "failed" {
			detail = "ok"
		}
	}
	return checkRow{Case: caseName, ID: id, Status: st, Detail: detail}
}

func findRegion(a *dto.AdminSkinReviewAnalysis, region string) *dto.AdminSkinAttentionArea {
	for i := range a.AttentionAreas {
		if strings.EqualFold(a.AttentionAreas[i].Region, region) {
			return &a.AttentionAreas[i]
		}
	}
	return nil
}

// looksNotVisible: note admits region is off-frame / no info / ask for fuller face.
func looksNotVisible(noteLower string) bool {
	cues := []string{
		"không có thông tin", "không thông tin", "ngoài khung", "không nằm trong khung",
		"không thấy trên ảnh", "không có trên ảnh", "không có trong ảnh", "không nằm trong ảnh",
		"không có trong khung", "không nằm trong ảnh này", "không có trên ảnh này",
		"chụp thêm", "chụp đủ mặt", "góc đủ mặt", "crop", "chỉ thấy trán",
		"not visible", "out of frame", "off-frame", "off frame", "no information", "no info",
		"không đoán", "không bịa", "không có cơ sở", "không nhận xét da",
		"thiếu mũi", "thiếu má", "thiếu cằm", "thiếu mặt",
	}
	for _, c := range cues {
		if strings.Contains(noteLower, c) {
			return true
		}
	}
	return false
}

// looksLikeFakeCalm: describes the off-frame region as visibly calm/clear.
func looksLikeFakeCalm(noteLower string) bool {
	if looksNotVisible(noteLower) {
		return false
	}
	fake := []string{
		"nhìn ổn", "trông ổn", "nhìn êm", "trông êm", "sạch nốt", "không thấy nốt",
		"không có nốt", "đang yên", "đang êm", "màu đều", "khá mịn", "không bóng",
		"không đỏ", "ổn áp", "looking clear", "looks fine", "no spots", "looks calm",
	}
	for _, f := range fake {
		if strings.Contains(noteLower, f) {
			return true
		}
	}
	return false
}

func hasConcreteSpotDetail(note string) bool {
	n := strings.ToLower(note)
	hasCount := regexp.MustCompile(`\d+|vài|cụm|rải|nhiều|một|hai|ba|bốn|năm|sáu|bảy|tám|chín|mười`).MatchString(n)
	hasColor := regexp.MustCompile(`đỏ|hồng|trắng|nâu|thâm|bóng|tươi|thẫm`).MatchString(n)
	hasLoc := regexp.MustCompile(`trán|má|mũi|cằm|cánh mũi|sống mũi|giữa trán|hai má|gần`).MatchString(n)
	return hasCount && hasColor && hasLoc
}

func senPhraseHits(a *dto.AdminSkinReviewAnalysis) []string {
	banned := []string{"ồn ào", "party", "drama", "lên tiếng", "bận rộn"}
	fields := userFacing(a)
	var hits []string
	for _, f := range fields {
		low := strings.ToLower(f.text)
		for _, b := range banned {
			if strings.Contains(low, strings.ToLower(b)) {
				hits = append(hits, fmt.Sprintf("%s contains %q", f.name, b))
			}
		}
	}
	return hits
}

func jargonHits(a *dto.AdminSkinReviewAnalysis) []string {
	patterns := []string{
		`(?i)\bpapules?\b`, `(?i)\bpustules?\b`, `(?i)\bcomedones?\b`, `(?i)\berythema\b`,
		`(?i)\bbarrier\b`, `(?i)\binflammat\w*\b`, `(?i)\bhyperpigmentation\b`, `(?i)\bsebum\b`,
		`(?i)\btexture\b`, `(?i)\bmorpholog\w*\b`, `(?i)\blesions?\b`, `(?i)\bclinical\b`,
		`(?i)\bbuccal\b`, `(?i)\bPIH\b`, `(?i)\bT-?zone\b`,
		`hàng rào da`,
	}
	fields := userFacing(a)
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
		`(?i)\broutine\b`, `(?i)\bserum\b`, `(?i)\bcleanser\b`, `(?i)\bmoisturizer\b`,
		`(?i)\bretinol\b`, `(?i)\bniacinamide\b`, `(?i)\bsunscreen\b`,
		`sản phẩm chăm sóc da`, `mỹ phẩm`, `nên dùng`, `nên thoa`, `nên bôi`, `hãy dùng`,
		`bước chăm sóc`, `kem dưỡng`, `kem chống nắng`, `sữa rửa mặt`,
	}
	fields := userFacing(a)
	// Also scan enum-ish free text fields for care advice phrases.
	fields = append(fields,
		field{"skin_type_note", a.SkinTypeNote},
	)
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

type field struct {
	name string
	text string
}

func userFacing(a *dto.AdminSkinReviewAnalysis) []field {
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
	return fields
}

func joinHits(hits []string) string {
	if len(hits) == 0 {
		return "ok"
	}
	return strings.Join(hits, "; ")
}

func compact(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
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

func printPromptSuggestions(rows []checkRow) {
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Status != "FAIL" {
			continue
		}
		var tip string
		switch {
		case r.ID == "A1.primary_forehead_not_chin":
			tip = "Siết FRAME LOCALIZATION: dải sát cạnh trên = forehead; CẤM chin trừ khi thấy môi/cằm. Few-shot Case 1 nhấn “không phải cằm”."
		case strings.HasPrefix(r.ID, "A.offframe"):
			tip = "Off-frame → concern=not_visible + note không có thông tin/ngoài khung/chụp đủ mặt; CẤM bịa ổn."
		case r.ID == "A.photo_notes_visibility":
			tip = "photo_notes: bắt buộc nêu crop một dải / chỉ thấy trán — không gọi nhầm cằm."
		case r.ID == "B2.nose_present_not_outside":
			tip = "NOSE rule: full face góc thẳng thấy sống/cánh mũi → concern none|real, CẤM not_visible oan."
		case r.ID == "B.problem_region_concrete":
			tip = "Case full-face: khi có nốt, note phải có số lượng ước lượng + màu + vị trí; fail nếu chỉ nói chung “có mụn”."
		case r.ID == "B.no_sen_phrases":
			tip = "Thêm ban list cứng vào schema Hard rules + few-shot không dùng ồn ào/party/drama/lên tiếng/bận rộn."
		case r.ID == "B.no_heavy_jargon":
			tip = "Nhắc lại: enum papules/pustules/texture/not_visible chỉ ở field concern; note = lời thường."
		case r.ID == "C.no_routine_product":
			tip = "Cấm tuyệt đối routine/sản phẩm đã có — siết banned list trong user message."
		default:
			tip = "Xem lại few-shot Case 1/2 trong admin_skin_review_prompt.go cho khớp checklist."
		}
		if seen[tip] {
			continue
		}
		seen[tip] = true
		fmt.Printf("- [%s] %s\n  → %s\n", r.ID, compact(r.Detail, 100), tip)
	}
}
