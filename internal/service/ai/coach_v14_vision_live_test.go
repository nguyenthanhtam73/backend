package ai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const coachLiveCompareTestTimeout = 60 * time.Minute

// coachCorePromptV17Archive — pre-v18 system prompt for A/B live comparison only.
const coachCorePromptV17Archive = `Bạn là DaDiary AI Skincare Coach — người bạn thân thiết luôn quan sát kỹ ảnh da và khích lệ user một cách chân thành.

Hôm nay mình nhìn kỹ ảnh và ghi chú của bạn rồi nhé. Mình sẽ nói thật lòng những gì mình thấy, không vòng vo.

## Giọng (BẮT BUỘC)
- Như nhắn tin bạn: "mình thấy", "hôm nay da bạn", "bạn đang làm khá tốt rồi đó", "mình khuyên thật lòng nhé", "nghe có vẻ…".
- Khen chân thành (effort, không ngoại hình) · khích lệ ấm · không sến · không hứa chữa khỏi.
- **Ngắn gọn** — mỗi câu súc tích, tránh lặp, tiết kiệm token.
- **Cấm:** báo cáo ("Phân tích cho thấy…"), liệt kê "1.2.3." / "T-zone:", câu chung ("da hơi khô", "cần dưỡng ẩm" không gắn vùng).

## Ảnh (BẮT BUỘC khi có VISION_SUMMARY_JSON)
- **≥4 chi tiết cụ thể** (vùng + dấu hiệu + mức) trong situation_analysis / concern_alignment — weave tự nhiên, không liệt kê khô.
- Mở situation_analysis: "Mình thấy hôm nay…" / "Hôm nay da bạn…". Không chẩn đoán, không kê thuốc.

## Lịch sử (BẮT BUỘC khi có ## Recent SkinChecks)
- ≥1 câu: "So với lần trước…" / "Vài hôm trước bạn cũng ghi…" / "Mấy lần gần đây…".

## Cấu trúc tự nhiên → JSON
1. Lời khen nhỏ → strengths
2. Mình thấy hôm nay da bạn thế nào (≥4 chi tiết ảnh) → situation_analysis + concern_alignment
3. So với lần trước → câu trong situation_analysis
4. Hôm nay mình khuyên bạn thử gì → improvements[].tip + routine_hints (Sáng:/Tối:)
5. Lý do + lưu ý an toàn → improvements[].why + avoid_or_patch + safety_reminders
6. Disclaimer nhẹ → medical_disclaimer + summary_notes

## USER_MEMORY
Callback bắt buộc · pivot 👎 · adherence + COACH_ACTION tier · không bịa brand.
Output: ONE JSON object per schema.`

func coachPromptV17Archive(skillLevel string) string {
	core := coachCorePromptV17Archive
	if strings.EqualFold(strings.TrimSpace(skillLevel), "beginner") {
		return core + "\n\n## BEGINNER\nTừ dễ · ≥4 chi tiết ảnh · strengths 1–3 · improvements 2–3."
	}
	return core + "\n\n## INTERMEDIATE/ADVANCED\nThuật ngữ OK · strengths 1–4 · improvements 2–5."
}

// TestCoachV18_BalancedLiveCompare runs 6 vision scenarios through v17 vs v18 prompts.
//
// Run: go test ./internal/service/ai/... -run TestCoachV18_BalancedLiveCompare -v -count=1 -timeout 60m
func TestCoachV18_BalancedLiveCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live v18 compare in short mode")
	}
	cfg := loadCoachTestConfig(t)
	if strings.TrimSpace(cfg.Anthropic.APIKey) == "" && strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		t.Skip("set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), coachLiveCompareTestTimeout-time.Minute)
	defer cancel()
	client := httpClientForCoachTests()

	sep := strings.Repeat("=", 130)
	t.Log(sep)
	t.Logf("Coach prompt A/B — v17 archive vs v18 (current) | scenarios=%d | timeout=%s",
		len(VisionCoachScenarios()), coachLiveCompareTestTimeout)
	t.Logf("v18 prompt chars (intermediate): %d", len(GetCoachPrompt("intermediate")))
	t.Log(sep)
	t.Log(fmt.Sprintf("%-18s | %-6s | vis | nat | emo | opn | enc | rpt | gen | hist | preview",
		"Scenario", "Prompt"))
	t.Log(strings.Repeat("-", 130))

	type row struct {
		id, prompt       string
		vision           int
		naturalness      float64
		emotional        float64
		opener, enc      bool
		report, generic  bool
		hist             bool
	}
	var rows []row

	for _, sc := range VisionCoachScenarios() {
		userMsg := sc.CoachUserMessage()
		for _, variant := range []struct {
			name   string
			system string
		}{
			{"v17", coachPromptV17Archive(sc.Persona.SkillLevel)},
			{"v18", GetCoachPrompt(sc.Persona.SkillLevel)},
		} {
			t.Run(sc.ID+"/"+variant.name, func(t *testing.T) {
				result, err := TextCoachCompletion(ctx, cfg, client, "v18-compare-"+variant.name, variant.system, userMsg)
				if err != nil {
					t.Fatalf("%s: %v", variant.name, err)
				}
				out, err := parseCoachStructuredOutput(result.Text, "v18-compare")
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				sc.Persona.VisionJSON = sc.VisionJSON
				pers := ScoreCoachPersonalization(sc.Persona, out, true)
				nat := ScoreCoachNaturalness(out)
				rows = append(rows, row{
					id: sc.ID, prompt: variant.name,
					vision: pers.VisionDetailCount, naturalness: nat.NaturalnessScore,
					emotional: nat.EmotionalScore,
					opener: nat.HasConversationalOpener, enc: nat.HasWarmEncouragement,
					report: pers.HasReportLikeTone, generic: pers.HasGenericPhrases,
					hist: pers.HasHistoryCallback,
				})
				t.Logf("%-18s | %-6s | %3d | %.2f | %.2f | %4v | %4v | %4v | %4v | %4v | %q",
					sc.ID, variant.name, pers.VisionDetailCount, nat.NaturalnessScore, nat.EmotionalScore,
					nat.HasConversationalOpener, nat.HasWarmEncouragement, pers.HasReportLikeTone,
					pers.HasGenericPhrases, pers.HasHistoryCallback, truncateRunes(out.SituationAnalysis, 85))
				t.Logf("  strengths: %q", truncateRunes(strings.Join(out.Strengths, " | "), 95))
				t.Logf("  closing:   %q", truncateRunes(out.SummaryNotes, 95))
			})
		}
	}

	t.Log("")
	t.Log("### Aggregate — naturalness & emotion (v17 vs v18)")
	type agg struct {
		n                                                       int
		vis                                                     int
		nat, emo                                                float64
		opn, enc, rep, gen, hist                                int
	}
	byPrompt := map[string]*agg{"v17": {}, "v18": {}}
	for _, r := range rows {
		a := byPrompt[r.prompt]
		a.n++
		a.vis += r.vision
		a.nat += r.naturalness
		a.emo += r.emotional
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
	for _, label := range []string{"v17", "v18"} {
		a := byPrompt[label]
		if a.n == 0 {
			continue
		}
		t.Logf("[%s] Avg vision: %.1f | naturalness: %.2f | emotional: %.2f | opener: %d/%d | warm-enc: %d/%d | report: %d/%d | generic: %d/%d | history: %d/%d",
			label,
			float64(a.vis)/float64(a.n), a.nat/float64(a.n), a.emo/float64(a.n),
			a.opn, a.n, a.enc, a.n, a.rep, a.n, a.gen, a.n, a.hist, a.n)
	}
	v17, v18 := byPrompt["v17"], byPrompt["v18"]
	if v17.n > 0 && v18.n > 0 {
		if float64(v18.vis)/float64(v18.n) < float64(MinVisionDetailCitations) {
			t.Errorf("v18 avg vision details below target %d", MinVisionDetailCitations)
		}
		natDelta := v18.nat/float64(v18.n) - v17.nat/float64(v17.n)
		emoDelta := v18.emo/float64(v18.n) - v17.emo/float64(v17.n)
		t.Logf("Delta v18-v17: naturalness %+.2f | emotional %+.2f", natDelta, emoDelta)
		if v18.enc < v18.n {
			t.Logf("WARN: v18 missing warm encouragement on %d/%d runs", v18.n-v18.enc, v18.n)
		}
		if emoDelta < 0 {
			t.Logf("WARN: v18 emotional score below v17 — review warm opener/closing enforcement")
		}
	}
	t.Log(sep)
}

func httpClientForCoachTests() *http.Client {
	return &http.Client{Timeout: 4 * time.Minute}
}
