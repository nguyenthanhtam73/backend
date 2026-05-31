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

// coachCorePromptV16Archive — pre-v17 system prompt for A/B live comparison only.
const coachCorePromptV16Archive = `Bạn là DaDiary AI Skincare Coach — người bạn thân thiết, luôn nhìn kỹ ảnh da và khích lệ user một cách chân thành.

Hôm nay hãy quan sát thật kỹ ảnh và ghi chú của user. Nói chuyện tự nhiên như đang nhắn tin cho bạn thân, đừng dùng giọng báo cáo hay liệt kê khô khan.

## Giọng văn (BẮT BUỘC — v16)
- Chat như bạn thân: "mình thấy", "hôm nay da bạn", "bạn đang làm tốt lắm", "mình khuyên thật lòng nhé".
- CẤM: "1. 2. 3.", "T-zone:", "Phân tích cho thấy", "Tình trạng da hiện tại".

## Quan sát ảnh (BẮT BUỘC — ≥3–4 chi tiết cụ thể)
- Weave ≥3–4 chi tiết (vùng + dấu hiệu + mức) vào situation_analysis/concern_alignment.
- Mở bằng "Mình thấy hôm nay…" / "Hôm nay da bạn…".
- CẤM HOÀN TOÀN: "da hơi khô", "da cần dưỡng ẩm" không gắn vùng.

## So sánh lịch sử (BẮT BUỘC khi có ## Recent SkinChecks)
- ≥1 câu "So với lần trước…" / "Vài hôm trước…"

## 6 bước → JSON
1. strengths 2. situation_analysis + concern_alignment 3. history callback
4. improvements + routine_hints 5. why + safety 6. disclaimer + summary_notes

## USER_MEMORY
Callback + COACH_ACTION adherence when sections present. Output: ONE JSON object per schema.`

func coachPromptV16Archive(skillLevel string) string {
	core := coachCorePromptV16Archive
	if strings.EqualFold(strings.TrimSpace(skillLevel), "beginner") {
		return core + "\n\n## BEGINNER\nCâu ngắn · ≥3–4 chi tiết ảnh · strengths 1–3 · improvements 2–3."
	}
	return core + "\n\n## INTERMEDIATE/ADVANCED\nVision ≥3–4 chi tiết · strengths 1–4 · improvements 2–5."
}

// TestCoachV17_WarmLiveCompare runs 6 vision scenarios through v16 vs v17 prompts.
//
// Run: go test ./internal/service/ai/... -run TestCoachV17_WarmLiveCompare -v -count=1 -timeout 60m
func TestCoachV17_WarmLiveCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live v17 compare in short mode")
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
	t.Logf("Coach prompt A/B — v16 archive vs v17 (current) | scenarios=%d | timeout=%s",
		len(VisionCoachScenarios()), coachLiveCompareTestTimeout)
	t.Logf("v17 prompt chars (intermediate): %d", len(GetCoachPrompt("intermediate")))
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
			{"v16", coachPromptV16Archive(sc.Persona.SkillLevel)},
			{"v17", GetCoachPrompt(sc.Persona.SkillLevel)},
		} {
			t.Run(sc.ID+"/"+variant.name, func(t *testing.T) {
				result, err := TextCoachCompletion(ctx, cfg, client, "v17-compare-"+variant.name, variant.system, userMsg)
				if err != nil {
					t.Fatalf("%s: %v", variant.name, err)
				}
				out, err := parseCoachStructuredOutput(result.Text, "v17-compare")
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
	t.Log("### Aggregate — naturalness & emotion (v16 vs v17)")
	type agg struct {
		n                                                                    int
		vis                                                                  int
		nat, emo                                                             float64
		opn, enc, rep, gen, hist                                             int
	}
	byPrompt := map[string]*agg{"v16": {}, "v17": {}}
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
	for _, label := range []string{"v16", "v17"} {
		a := byPrompt[label]
		if a.n == 0 {
			continue
		}
		t.Logf("[%s] Avg vision: %.1f | naturalness: %.2f | emotional: %.2f | opener: %d/%d | warm-enc: %d/%d | report: %d/%d | generic: %d/%d | history: %d/%d",
			label,
			float64(a.vis)/float64(a.n), a.nat/float64(a.n), a.emo/float64(a.n),
			a.opn, a.n, a.enc, a.n, a.rep, a.n, a.gen, a.n, a.hist, a.n)
	}
	v16, v17 := byPrompt["v16"], byPrompt["v17"]
	if v16.n > 0 && v17.n > 0 {
		if float64(v17.vis)/float64(v17.n) < float64(MinVisionDetailCitations) {
			t.Errorf("v17 avg vision details below target %d", MinVisionDetailCitations)
		}
		natDelta := v17.nat/float64(v17.n) - v16.nat/float64(v16.n)
		emoDelta := v17.emo/float64(v17.n) - v16.emo/float64(v16.n)
		t.Logf("Delta v17-v16: naturalness %+.2f | emotional %+.2f", natDelta, emoDelta)
		if v17.opn < v17.n {
			t.Logf("WARN: v17 missing conversational opener on %d/%d runs", v17.n-v17.opn, v17.n)
		}
	}
	t.Log(sep)
}

func httpClientForCoachTests() *http.Client {
	return &http.Client{Timeout: 4 * time.Minute}
}
