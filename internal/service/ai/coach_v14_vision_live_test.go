package ai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// coachCorePromptV15Archive is the pre-v16 system prompt kept for A/B live comparison only.
const coachCorePromptV15Archive = `Bạn là DaDiary AI Skincare Coach — người bạn gần gũi, quan sát kỹ và luôn khích lệ.

Hôm nay hãy nhìn thật kỹ vào ảnh da của user và ghi chú của họ. Đừng nói chung chung.

## Giọng văn
- Nói như chat với bạn thân — không viết như báo cáo hay checklist khô.
- Weave ≥3 chi tiết ảnh vào câu tự nhiên ("mình thấy…", "trên ảnh…").
- CẤM: "da hơi khô", "da cần dưỡng ẩm" không gắn vùng.

## So sánh lịch sử (BẮT BUỘC khi có ## Recent SkinChecks)
- ≥1 câu "So với mấy lần gần đây…" / "Vài hôm trước…"

## 6 bước → JSON
1. strengths 2. situation_analysis + concern_alignment 3. history callback
4. improvements + routine_hints 5. why + safety 6. disclaimer + summary_notes

## USER_MEMORY
Callback + adherence when sections present. Output: ONE JSON object per schema.`

// TestCoachV16_BestFriendLiveCompare runs 6 vision scenarios through v15 vs v16 prompts.
//
// Run: go test ./internal/service/ai/... -run TestCoachV16_BestFriendLiveCompare -v -count=1 -timeout 30m
func TestCoachV16_BestFriendLiveCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live v16 compare in short mode")
	}
	cfg := loadCoachTestConfig(t)
	if strings.TrimSpace(cfg.Anthropic.APIKey) == "" && strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		t.Skip("set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 28*time.Minute)
	defer cancel()
	client := httpClientForCoachTests()

	sep := strings.Repeat("=", 118)
	t.Log(sep)
	t.Logf("Coach prompt A/B — v15 archive vs v16 (current) | scenarios=%d", len(VisionCoachScenarios()))
	t.Log(sep)
	t.Log(fmt.Sprintf("%-18s | %-6s | vis | nat | opn | rpt | gen | hist | preview",
		"Scenario", "Prompt"))
	t.Log(strings.Repeat("-", 118))

	type row struct {
		id, prompt       string
		vision           int
		naturalness      float64
		opener, report   bool
		generic, hist    bool
	}
	var rows []row

	for _, sc := range VisionCoachScenarios() {
		userMsg := sc.CoachUserMessage()
		for _, variant := range []struct {
			name   string
			system string
		}{
			{"v15", coachCorePromptV15Archive},
			{"v16", GetCoachPrompt(sc.Persona.SkillLevel)},
		} {
			t.Run(sc.ID+"/"+variant.name, func(t *testing.T) {
				result, err := TextCoachCompletion(ctx, cfg, client, "v16-compare-"+variant.name, variant.system, userMsg)
				if err != nil {
					t.Fatalf("%s: %v", variant.name, err)
				}
				out, err := parseCoachStructuredOutput(result.Text, "v16-compare")
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				sc.Persona.VisionJSON = sc.VisionJSON
				pers := ScoreCoachPersonalization(sc.Persona, out, true)
				nat := ScoreCoachNaturalness(out)
				rows = append(rows, row{
					id: sc.ID, prompt: variant.name,
					vision: pers.VisionDetailCount, naturalness: nat.NaturalnessScore,
					opener: nat.HasConversationalOpener, report: pers.HasReportLikeTone,
					generic: pers.HasGenericPhrases, hist: pers.HasHistoryCallback,
				})
				t.Logf("%-18s | %-6s | %3d | %.2f | %4v | %4v | %4v | %4v | %q",
					sc.ID, variant.name, pers.VisionDetailCount, nat.NaturalnessScore,
					nat.HasConversationalOpener, pers.HasReportLikeTone, pers.HasGenericPhrases,
					pers.HasHistoryCallback, truncateRunes(out.SituationAnalysis, 85))
				t.Logf("  strengths: %q", truncateRunes(strings.Join(out.Strengths, " | "), 95))
				t.Logf("  closing:   %q", truncateRunes(out.SummaryNotes, 95))
			})
		}
	}

	t.Log("")
	t.Log("### Aggregate")
	var v15Vis, v16Vis int
	var v15Nat, v16Nat float64
	var v15Opn, v16Opn, v15Rep, v16Rep, v15Gen, v16Gen, v15Hist, v16Hist int
	var v15N, v16N int
	for _, r := range rows {
		switch r.prompt {
		case "v15":
			v15Vis += r.vision
			v15Nat += r.naturalness
			if r.opener {
				v15Opn++
			}
			if r.report {
				v15Rep++
			}
			if r.generic {
				v15Gen++
			}
			if r.hist {
				v15Hist++
			}
			v15N++
		case "v16":
			v16Vis += r.vision
			v16Nat += r.naturalness
			if r.opener {
				v16Opn++
			}
			if r.report {
				v16Rep++
			}
			if r.generic {
				v16Gen++
			}
			if r.hist {
				v16Hist++
			}
			v16N++
		}
	}
	if v15N > 0 && v16N > 0 {
		t.Logf("Avg vision details:     v15=%.1f  v16=%.1f  (target ≥%d)",
			float64(v15Vis)/float64(v15N), float64(v16Vis)/float64(v16N), MinVisionDetailCitations)
		t.Logf("Avg naturalness:        v15=%.2f  v16=%.2f",
			v15Nat/float64(v15N), v16Nat/float64(v16N))
		t.Logf("Conversational opener:  v15=%d/%d  v16=%d/%d", v15Opn, v15N, v16Opn, v16N)
		t.Logf("Report-like hits:       v15=%d/%d  v16=%d/%d", v15Rep, v15N, v16Rep, v16N)
		t.Logf("Generic phrase hits:    v15=%d/%d  v16=%d/%d", v15Gen, v15N, v16Gen, v16N)
		t.Logf("History callbacks:      v15=%d/%d  v16=%d/%d", v15Hist, v15N, v16Hist, v16N)
		if float64(v16Vis)/float64(v16N) < float64(MinVisionDetailCitations) {
			t.Errorf("v16 avg vision details below target %d", MinVisionDetailCitations)
		}
		if v16Opn < v16N {
			t.Logf("WARN: v16 missing conversational opener on %d/%d runs", v16N-v16Opn, v16N)
		}
	}
	t.Log(sep)
}

func httpClientForCoachTests() *http.Client {
	return &http.Client{Timeout: 4 * time.Minute}
}
