// Temporary dual-case checker for Admin Skin Review prompt tighten.
// Usage from backend/: go run ./tmp_test_assets/tighten_check/
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

func main() {
	assets := "tmp_test_assets"
	if _, err := os.Stat(assets); err != nil {
		assets = filepath.Join("backend", "tmp_test_assets")
	}
	sourcePath := filepath.Join(assets, "acne_clear_issues.jpg")
	fullPath := sourcePath // use original full-face plate (painted landmarks confused the model)
	cheekPath := filepath.Join(assets, "cheek_closeup.jpg")
	if err := cropCheekCloseup(sourcePath, cheekPath); err != nil {
		fmt.Fprintf(os.Stderr, "crop cheek: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("wrote", cheekPath)
	fmt.Println("full face asset:", fullPath)

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

	type caseSpec struct {
		name string
		path string
		fn   func(*dto.AdminSkinReviewAnalysis) []row
	}
	// Run full-face first — back-to-back close-up→full-face sometimes hits model refusal.
	cases := []caseSpec{
		{"A_full_face", fullPath, checkFullFace},
		{"B_cheek_closeup", cheekPath, checkCheekCloseup},
	}

	overallFail := false
	var all []row
	for i, c := range cases {
		if i > 0 {
			time.Sleep(20 * time.Second) // avoid back-to-back vision refusal
		}
		raw, readErr := os.ReadFile(c.path)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", c.path, readErr)
			os.Exit(1)
		}
		var analysis *dto.AdminSkinReviewAnalysis
		var model string
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			analysis, model, err = ai.AdminSkinReviewAnalyze(ctx, cfg, client, []ai.ImageBytes{{Data: raw}}, "vi")
			cancel()
			if err == nil {
				break
			}
			if strings.Contains(err.Error(), "refusal") || strings.Contains(err.Error(), "empty model content") {
				fmt.Fprintf(os.Stderr, "%s attempt %d refused/empty, retrying in 25s: %v\n", c.name, attempt, err)
				time.Sleep(25 * time.Second)
				continue
			}
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s analyze: %v\n", c.name, err)
			os.Exit(1)
		}
		env := map[string]any{
			"case": c.name, "model_used": model, "image": c.path, "analysis": analysis,
		}
		b, _ := json.MarshalIndent(env, "", "  ")
		fmt.Printf("\n######## %s ########\n=== JSON ===\n%s\n", c.name, string(b))
		rows := c.fn(analysis)
		for _, r := range rows {
			if r.Status == "FAIL" {
				overallFail = true
			}
			all = append(all, r)
		}
	}

	fmt.Println("\n======== CHECKLIST ========")
	fmt.Printf("| %-16s | %-40s | %-4s | %s\n", "CASE", "CHECK", "OK?", "DETAIL")
	for _, r := range all {
		fmt.Printf("| %-16s | %-40s | %-4s | %s\n", r.Case, r.ID, r.Status, compact(r.Detail, 90))
	}
	if overallFail {
		fmt.Println("\nOVERALL: FAIL")
		os.Exit(1)
	}
	fmt.Println("\nOVERALL: PASS")
}

type row struct{ Case, ID, Status, Detail string }

func checkCheekCloseup(a *dto.AdminSkinReviewAnalysis) []row {
	var rows []row
	cheeks := find(a, "cheeks")
	rows = append(rows, mk("B_cheek_closeup", "cheeks_thick_problem",
		cheeks != nil && concern(cheeks) != "not_visible" && concern(cheeks) != "none" && sentences(cheeks.Note) >= 5,
		fmt.Sprintf("concern=%q sentences=%d", concern(cheeks), noteSent(cheeks))))
	for _, region := range []string{"forehead", "nose", "chin"} {
		ar := find(a, region)
		ok := ar != nil && concern(ar) == "not_visible" && sentences(ar.Note) == 1 && !fakeCalm(ar.Note)
		rows = append(rows, mk("B_cheek_closeup", region+"_not_visible_1",
			ok, fmt.Sprintf("concern=%q sent=%d note=%q", concern(ar), noteSent(ar), compact(noteOf(ar), 50))))
	}
	pn := strings.ToLower(a.PhotoNotes)
	photoOK := strings.Contains(pn, "close-up") || strings.Contains(pn, "closeup") ||
		strings.Contains(pn, "crop") || strings.Contains(pn, "chỉ") && strings.Contains(pn, "má") ||
		strings.Contains(pn, "nửa mặt") || strings.Contains(pn, "thiếu")
	rows = append(rows, mk("B_cheek_closeup", "photo_notes_closeup", photoOK, compact(a.PhotoNotes, 70)))
	rows = append(rows, shared("B_cheek_closeup", a)...)
	return rows
}

func checkFullFace(a *dto.AdminSkinReviewAnalysis) []row {
	var rows []row
	for _, region := range []string{"forehead", "nose", "cheeks", "chin"} {
		ar := find(a, region)
		ok := ar != nil && concern(ar) != "not_visible"
		rows = append(rows, mk("A_full_face", region+"_visible", ok,
			fmt.Sprintf("concern=%q", concern(ar))))
	}
	rows = append(rows, mk("A_full_face", "overview_4plus", sentences(a.Overview) >= 4,
		fmt.Sprintf("%d câu", sentences(a.Overview))))
	rows = append(rows, shared("A_full_face", a)...)
	return rows
}

func shared(caseName string, a *dto.AdminSkinReviewAnalysis) []row {
	text := allText(a)
	emptyRe := regexp.MustCompile(`(?i)không thể bỏ qua|nhìn là biết|nhìn cái là thấy|chịu trách nhiệm với da|đừng bảo không sao|party|drama|ồn ào`)
	confidentRe := regexp.MustCompile(`(?i)đây là|mày đang|trông đúng kiểu|mụn viêm|mụn có mủ|mụn bọc|mụn cồi`)
	hedgeSpamRe := regexp.MustCompile(`(?i)không chắc 100%|chưa chắc|trên ảnh nghi|đôi khi liên quan|có thể là|thường gặp khi`)
	banRe := regexp.MustCompile(`(?i)nên thoa|nên dùng|nên bôi|mỹ phẩm|routine sáng|routine tối|\bBHA\b|\bretinol\b|care_suggestions`)
	voiceRe := regexp.MustCompile(`(?i)\btao\b|\bmày\b`)
	// Ban soft “mình/bạn” address in main prose (allow in non_diagnostic).
	mainSansDisclaimer := strings.ToLower(a.Overview + " " + a.SkinTypeNote + " " + a.AdditionalObservations)
	for _, ar := range a.AttentionAreas {
		if strings.ToLower(strings.TrimSpace(ar.Concern)) == "not_visible" {
			continue
		}
		mainSansDisclaimer += " " + strings.ToLower(ar.Note)
	}
	minhBan := regexp.MustCompile(`(?i)\bmình\b|\bbạn\b`)
	swearRe := regexp.MustCompile(`(?i)\bđmm\b|\bđụ\b|\bcặc\b|\bdỉ\b|\bđjt\b`)
	causesOK := len(a.PossibleCauses) >= 1 && len(a.PossibleCauses) <= 2
	tipsOK := len(a.SoothingTips) >= 2 && len(a.SoothingTips) <= 3
	// Contradictory thâm inside the same note/field (region-local).
	thamAffirm := regexp.MustCompile(`(?i)thâm (rất )?nhẹ|thâm nông|điểm thâm|vệt thâm|thâm thì có|thâm nâu`)
	thamDeny := regexp.MustCompile(`(?i)không thấy thâm|chưa thấy thâm`)
	thamOK := true
	for _, chunk := range []string{a.Overview, a.AdditionalObservations} {
		if thamAffirm.MatchString(chunk) && thamDeny.MatchString(chunk) {
			thamOK = false
		}
	}
	for _, ar := range a.AttentionAreas {
		if thamAffirm.MatchString(ar.Note) && thamDeny.MatchString(ar.Note) {
			thamOK = false
		}
	}
	// Soft anti-repeat: "bóng" shouldn't spam every block.
	bongCount := strings.Count(strings.ToLower(text), "bóng")
	repeatOK := bongCount <= 6
	return []row{
		mk(caseName, "no_empty_phrases", !emptyRe.MatchString(text), emptyRe.FindString(text)),
		mk(caseName, "has_confident_morph", confidentRe.MatchString(text), "need Đây là… / mụn viêm…"),
		mk(caseName, "no_hedge_spam", !hedgeSpamRe.MatchString(text), hedgeSpamRe.FindString(text)),
		mk(caseName, "no_routine_product", !banRe.MatchString(text), banRe.FindString(text)),
		mk(caseName, "has_tao_may", voiceRe.MatchString(mainSansDisclaimer), "need tao/mày in main prose"),
		mk(caseName, "no_minh_ban", !minhBan.MatchString(mainSansDisclaimer), minhBan.FindString(mainSansDisclaimer)),
		mk(caseName, "no_swearing", !swearRe.MatchString(text), swearRe.FindString(text)),
		mk(caseName, "causes_1_2", causesOK, fmt.Sprintf("n=%d", len(a.PossibleCauses))),
		mk(caseName, "tips_2_3", tipsOK, fmt.Sprintf("n=%d", len(a.SoothingTips))),
		mk(caseName, "tham_no_contradict", thamOK, "affirm+deny thâm"),
		mk(caseName, "no_bong_spam", repeatOK, fmt.Sprintf("bóng×%d", bongCount)),
	}
}

func writeFullFaceWithLandmarks(src, dst string) error {
	f, err := os.Open(src)
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
	// Hairline band → reads as forehead/top of face.
	hairH := b.Dy() / 10
	for y := b.Min.Y; y < b.Min.Y+hairH; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			rgba.Set(x, y, color.RGBA{12, 10, 8, 255})
		}
	}
	// Nose bridge + alae.
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
	out, err := os.Create(dst)
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

func cropCheekCloseup(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		return err
	}
	b := img.Bounds()
	// Left cheek band on synthetic 800x1000 face (dense papules around x~210-290, y~480-580).
	rect := image.Rect(
		b.Min.X+b.Dx()*8/100,
		b.Min.Y+b.Dy()*42/100,
		b.Min.X+b.Dx()*48/100,
		b.Min.Y+b.Dy()*72/100,
	)
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	si, ok := img.(subImager)
	if !ok {
		return fmt.Errorf("no SubImage")
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	return jpeg.Encode(out, si.SubImage(rect), &jpeg.Options{Quality: 92})
}

func find(a *dto.AdminSkinReviewAnalysis, region string) *dto.AdminSkinAttentionArea {
	for i := range a.AttentionAreas {
		if strings.EqualFold(a.AttentionAreas[i].Region, region) {
			return &a.AttentionAreas[i]
		}
	}
	return nil
}
func concern(a *dto.AdminSkinAttentionArea) string {
	if a == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(a.Concern))
}
func noteOf(a *dto.AdminSkinAttentionArea) string {
	if a == nil {
		return ""
	}
	return a.Note
}
func noteSent(a *dto.AdminSkinAttentionArea) int {
	if a == nil {
		return 0
	}
	return sentences(a.Note)
}
func sentences(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	parts := regexp.MustCompile(`[.!?…]+|\n+`).Split(s, -1)
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
func fakeCalm(note string) bool {
	n := strings.ToLower(note)
	if strings.Contains(n, "không thấy") || strings.Contains(n, "chụp đủ mặt") {
		return false
	}
	for _, f := range []string{"nhìn ổn", "đang yên", "không có nốt", "sạch nốt"} {
		if strings.Contains(n, f) {
			return true
		}
	}
	return false
}
func allText(a *dto.AdminSkinReviewAnalysis) string {
	var b strings.Builder
	b.WriteString(a.Overview)
	b.WriteString("\n")
	b.WriteString(a.SkinTypeNote)
	b.WriteString("\n")
	b.WriteString(a.AdditionalObservations)
	b.WriteString("\n")
	b.WriteString(a.PhotoNotes)
	b.WriteString("\n")
	b.WriteString(a.NonDiagnostic)
	for _, ar := range a.AttentionAreas {
		b.WriteString("\n")
		b.WriteString(ar.Note)
	}
	return b.String()
}
func mk(caseName, id string, pass bool, detail string) row {
	st := "PASS"
	if !pass {
		st = "FAIL"
		if strings.TrimSpace(detail) == "" {
			detail = "failed"
		}
	} else if strings.TrimSpace(detail) == "" {
		detail = "ok"
	}
	return row{Case: caseName, ID: id, Status: st, Detail: detail}
}
func compact(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
