package ai

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
)

const coachLiveCompareTestTimeout = 60 * time.Minute

// coachCorePromptV20Archive — pre-v21 system prompt for A/B live comparison only.
const coachCorePromptV20Archive = `Bạn là DaDiary AI Skincare Coach — người bạn thân thiết, quan sát cực kỹ ảnh da và nói thật lòng, cụ thể với user.

Hôm nay mình zoom rất kỹ vào ảnh rồi. Mình sẽ nói rõ những gì mình thấy, không nói chung chung kiểu "da hỗn hợp" hay "dễ nổi mụn".

## Giọng (BẮT BUỘC)
- Gần gũi, chân thành, cụ thể — không từ mơ hồ, không lạnh/khách quan.
- **Cấm hoàn toàn:** "da hỗn hợp", "da dễ nổi mụn", "dễ nổi mụn", "da hơi khô", "cần dưỡng ẩm", "sản phẩm nhẹ nhàng", "chăm sóc nhẹ", "không đều màu" (không gắn vùng).
- **Cấm:** báo cáo ("Phân tích cho thấy…"), liệt kê "1.2.3." khô.

## Ảnh (BẮT BUỘC)
- **≥4–6 chi tiết cụ thể** — vùng da + dấu hiệu + mức (+ số lượng nếu thấy).
- **Bắt buộc mở bằng:** "Mình thấy hôm nay…" / "Trên ảnh mình thấy vùng …" / "Có … nốt mụn ở …"

## Lịch sử (BẮT BUỘC khi có ## Recent SkinChecks)
- ≥1 câu: "So với lần trước…"

## Cấu trúc → JSON per schema. USER_MEMORY + disclaimer.`

func coachPromptV20Archive(skillLevel string) string {
	core := coachCorePromptV20Archive
	if strings.EqualFold(strings.TrimSpace(skillLevel), "beginner") {
		return core + "\n\n## BEGINNER\n≥4 chi tiết ảnh cụ thể."
	}
	return core + "\n\n## INTERMEDIATE\n≥4–6 chi tiết · gợi ý cụ thể."
}

// coachCorePromptV19Archive — pre-v20 system prompt for A/B live comparison only.
const coachCorePromptV19Archive = `Bạn là DaDiary AI Skincare Coach — người bạn thân thiết, quan sát rất kỹ ảnh da và nói thật lòng với user.

Hôm nay mình đã zoom kỹ vào ảnh da của bạn rồi. Mình sẽ nói cụ thể những gì mình thấy, không nói chung chung.

## Ảnh (BẮT BUỘC)
- ≥4–5 chi tiết cụ thể · mở "Mình thấy hôm nay…" / "Trên ảnh mình thấy…" / "Vùng … của bạn…"
- Cấm "sản phẩm nhẹ nhàng", "da hơi khô" không gắn vùng.
- So với lần trước khi có ## Recent SkinChecks.

## USER_MEMORY + JSON output per schema.`

func coachPromptV19Archive(skillLevel string) string {
	core := coachCorePromptV19Archive
	if strings.EqualFold(strings.TrimSpace(skillLevel), "beginner") {
		return core + "\n\n## BEGINNER\n≥4 chi tiết ảnh cụ thể."
	}
	return core + "\n\n## INTERMEDIATE\n≥4–5 chi tiết · gợi ý cụ thể."
}

type coachCompareRow struct {
	id, prompt       string
	vision           int
	naturalness      float64
	emotional        float64
	buca             float64
	opener, enc      bool
	report, generic  bool
	hist             bool
}

func runCoachCompare(t *testing.T, ctx context.Context, cfg *config.Config, client *http.Client, scenarios []VisionCoachScenario, variants []struct {
	name   string
	system func(string) string
}) []coachCompareRow {
	t.Helper()
	var rows []coachCompareRow
	for _, sc := range scenarios {
		userMsg := sc.CoachUserMessage()
		for _, variant := range variants {
			t.Run(sc.ID+"/"+variant.name, func(t *testing.T) {
				system := variant.system(sc.Persona.SkillLevel)
				result, err := TextCoachCompletion(ctx, cfg, client, "v20-compare-"+variant.name, system, userMsg)
				if err != nil {
					t.Fatalf("%s: %v", variant.name, err)
				}
				out, err := parseCoachStructuredOutput(result.Text, "v20-compare")
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				sc.Persona.VisionJSON = sc.VisionJSON
				pers := ScoreCoachPersonalization(sc.Persona, out, true)
				nat := ScoreCoachNaturalness(out)
				buca := ScoreCoachBucaTone(out)
				flat := FlattenCoachOutput(out)
				rows = append(rows, coachCompareRow{
					id: sc.ID, prompt: variant.name,
					vision: pers.VisionDetailCount, naturalness: nat.NaturalnessScore,
					emotional: nat.EmotionalScore, buca: buca.Score,
					opener: outputHasRequiredVisionOpener(out), enc: nat.HasWarmEncouragement,
					report: pers.HasReportLikeTone,
					generic: pers.HasGenericPhrases || outputHasBannedGenericLabels(flat) || outputHasVagueTipPhrases(out),
					hist: pers.HasHistoryCallback,
				})
				t.Logf("%-22s | %-6s | %3d | %.2f | %.2f | %.2f | %4v | %4v | %4v | %4v | %4v | %q",
					sc.ID, variant.name, pers.VisionDetailCount, nat.NaturalnessScore, nat.EmotionalScore, buca.Score,
					outputHasRequiredVisionOpener(out), nat.HasWarmEncouragement, pers.HasReportLikeTone,
					pers.HasGenericPhrases || outputHasBannedGenericLabels(flat), pers.HasHistoryCallback,
					truncateRunes(out.SituationAnalysis, 85))
				t.Logf("  strengths: %q", truncateRunes(strings.Join(out.Strengths, " | "), 95))
				t.Logf("  tip:       %q", truncateRunes(firstImprovementTip(out), 95))
				t.Logf("  closing:   %q", truncateRunes(out.SummaryNotes, 95))
			})
		}
	}
	return rows
}

func firstImprovementTip(out *CoachStructuredOutput) string {
	if out == nil || len(out.Improvements) == 0 {
		return ""
	}
	return out.Improvements[0].Tip
}

func logCoachCompareAggregate(t *testing.T, rows []coachCompareRow, left, right string) {
	t.Helper()
	type agg struct {
		n                                        int
		vis                                      int
		nat, emo, buca                           float64
		opn, enc, rep, gen, hist                 int
	}
	byPrompt := map[string]*agg{left: {}, right: {}}
	for _, r := range rows {
		a := byPrompt[r.prompt]
		a.n++
		a.vis += r.vision
		a.nat += r.naturalness
		a.emo += r.emotional
		a.buca += r.buca
		if r.opener {
			a.opn++
		}
		if r.enc {
			a.enc++
		}
		if r.report {
			a.rep++
		}
		if r.generic {
			a.gen++
		}
		if r.hist {
			a.hist++
		}
	}
	for _, label := range []string{left, right} {
		a := byPrompt[label]
		if a.n == 0 {
			continue
		}
		t.Logf("[%s] Avg vision: %.1f | naturalness: %.2f | emotional: %.2f | buca: %.2f | opener: %d/%d | warm-enc: %d/%d | report: %d/%d | generic: %d/%d | history: %d/%d",
			label,
			float64(a.vis)/float64(a.n), a.nat/float64(a.n), a.emo/float64(a.n), a.buca/float64(a.n),
			a.opn, a.n, a.enc, a.n, a.rep, a.n, a.gen, a.n, a.hist, a.n)
	}
	l, r := byPrompt[left], byPrompt[right]
	if l.n > 0 && r.n > 0 {
		t.Logf("Delta %s-%s: vision %+.1f | naturalness %+.2f | emotional %+.2f | buca %+.2f | generic %d→%d",
			right, left,
			float64(r.vis)/float64(r.n)-float64(l.vis)/float64(l.n),
			r.nat/float64(r.n)-l.nat/float64(l.n),
			r.emo/float64(r.n)-l.emo/float64(l.n),
			r.buca/float64(r.n)-l.buca/float64(l.n),
			l.gen, r.gen)
	}
}

// TestCoachV20_UserPhotoLiveCompare runs 2 user cheek photo scenarios (v19 vs v20).
//
// Run: go test ./internal/service/ai/... -run TestCoachV20_UserPhotoLiveCompare -v -count=1 -timeout 60m
func TestCoachV20_UserPhotoLiveCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live v20 user photo compare in short mode")
	}
	cfg := loadCoachTestConfig(t)
	if strings.TrimSpace(cfg.Anthropic.APIKey) == "" && strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		t.Skip("set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), coachLiveCompareTestTimeout-time.Minute)
	defer cancel()
	client := httpClientForCoachTests()

	scenarios := UserPhotoCoachScenarios()
	sep := strings.Repeat("=", 130)
	t.Log(sep)
	t.Logf("User photo A/B — v19 archive vs v20 (current) | scenarios=%d (%s, %s)",
		len(scenarios), scenarios[0].ID, scenarios[1].ID)
	t.Logf("v20 prompt chars (intermediate): %d | MaxCoachValidationRetries=%d",
		len(GetCoachPrompt("intermediate")), MaxCoachValidationRetries)
	t.Log(sep)

	variants := []struct {
		name   string
		system func(string) string
	}{
		{"v19", coachPromptV19Archive},
		{"v20", GetCoachPrompt},
	}
	rows := runCoachCompare(t, ctx, cfg, client, scenarios, variants)

	t.Log("")
	t.Log("### Aggregate — user cheek photos (v19 vs v20)")
	logCoachCompareAggregate(t, rows, "v19", "v20")

	for _, r := range rows {
		if r.prompt != "v20" {
			continue
		}
		if r.vision < MinVisionDetailCitations {
			t.Errorf("%s: v20 vision details %d below target %d", r.id, r.vision, MinVisionDetailCitations)
		}
		if !r.hist {
			t.Errorf("%s: v20 missing history callback", r.id)
		}
		if !r.opener {
			t.Errorf("%s: v20 missing required vision opener", r.id)
		}
		if r.generic {
			t.Errorf("%s: v20 hit banned generic/vague phrases", r.id)
		}
	}
	t.Log(sep)
}

// TestCoachV21_BucaLiveCompare runs oily/acne + severe acne + user cheek scenarios (v20 vs v21).
//
// Run: go test ./internal/service/ai/... -run TestCoachV21_BucaLiveCompare -v -count=1 -timeout 60m
func TestCoachV21_BucaLiveCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live v21 buca compare in short mode")
	}
	cfg := loadCoachTestConfig(t)
	if strings.TrimSpace(cfg.Anthropic.APIKey) == "" && strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		t.Skip("set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), coachLiveCompareTestTimeout-time.Minute)
	defer cancel()
	client := httpClientForCoachTests()

	all := VisionCoachScenarios()
	var scenarios []VisionCoachScenario
	for _, sc := range all {
		if sc.ID == "oily_acne" || sc.ID == "severe_acne" {
			scenarios = append(scenarios, sc)
		}
	}
	scenarios = append(scenarios, UserPhotoCoachScenarios()...)

	sep := strings.Repeat("=", 130)
	t.Log(sep)
	t.Logf("Buca A/B — v20 archive vs v21 (current) | scenarios=%d | prompt v%d",
		len(scenarios), CoachDailyPromptVersion)
	t.Logf("v21 prompt chars (intermediate): %d", len(GetCoachPrompt("intermediate")))
	t.Log(sep)

	variants := []struct {
		name   string
		system func(string) string
	}{
		{"v20", coachPromptV20Archive},
		{"v21", GetCoachPrompt},
	}
	rows := runCoachCompare(t, ctx, cfg, client, scenarios, variants)

	t.Log("")
	t.Log("### Aggregate — buca tone compare (v20 vs v21)")
	logCoachCompareAggregate(t, rows, "v20", "v21")

	for _, r := range rows {
		if r.prompt != "v21" {
			continue
		}
		if r.vision < MinVisionDetailCitations {
			t.Errorf("%s: v21 vision details %d below target %d", r.id, r.vision, MinVisionDetailCitations)
		}
		if !r.hist {
			t.Errorf("%s: v21 missing history callback", r.id)
		}
		if !r.opener {
			t.Errorf("%s: v21 missing required vision opener", r.id)
		}
		if r.generic {
			t.Errorf("%s: v21 hit banned generic/vague phrases", r.id)
		}
		if r.buca < 0.25 {
			t.Errorf("%s: v21 buca score %.2f too low (want ≥0.25)", r.id, r.buca)
		}
	}
	t.Log(sep)
}

func httpClientForCoachTests() *http.Client {
	return &http.Client{Timeout: 4 * time.Minute}
}
