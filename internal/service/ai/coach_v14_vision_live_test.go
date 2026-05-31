package ai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// coachCorePromptV12Archive is the pre-v14 system prompt kept for A/B live comparison only.
const coachCorePromptV12Archive = `Bạn là DaDiary AI Skincare Coach — người bạn đồng hành ấm áp, chân thành, kiên nhẫn và dễ hiểu nhất.

## Phong cách
- Nói như bạn bè gần gũi, khích lệ nhiều, không phán xét.
- Ngắn gọn, dễ đọc trên điện thoại. Mỗi câu phải cụ thể.

## Quy tắc cốt lõi
- Luôn có lời khen nhỏ — chân thực, khích lệ effort (ghi nhật ký, kiên trì).
- **USER_MEMORY là bắt buộc đọc kỹ**.
- Không chẩn đoán bệnh, không kê thuốc.

## 6 bước → JSON
1. Lời khen nhỏ → strengths
2. Tóm tắt da hôm nay → situation_analysis
3. Gợi ý hôm nay → improvements + routine_hints
4. Lý do → improvements.why
5. An toàn → avoid_or_patch + safety_reminders
6. Disclaimer → medical_disclaimer + summary_notes

Output: exactly ONE JSON object per schema in user message.`

// TestCoachV14_VisionLiveCompare runs 4 vision scenarios through v12 vs v14 prompts
// and logs vision detail counts, generic phrase hits, and history callbacks.
//
// Run: go test ./internal/service/ai/... -run TestCoachV14_VisionLiveCompare -v -count=1 -timeout 20m
func TestCoachV14_VisionLiveCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live v14 vision compare in short mode")
	}
	cfg := loadCoachTestConfig(t)
	if strings.TrimSpace(cfg.Anthropic.APIKey) == "" && strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		t.Skip("set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()
	client := httpClientForCoachTests()

	sep := strings.Repeat("=", 96)
	t.Log(sep)
	t.Logf("Coach prompt A/B — v12 archive vs v14 (current) | scenarios=%d", len(VisionCoachScenarios()))
	t.Log(sep)
	t.Log(fmt.Sprintf("%-18s | %-6s | vision | generic | hist | preview",
		"Scenario", "Prompt"))
	t.Log(strings.Repeat("-", 96))

	type row struct {
		id, prompt string
		vision     int
		generic    bool
		hist       bool
		preview    string
	}
	var rows []row

	for _, sc := range VisionCoachScenarios() {
		userMsg := sc.CoachUserMessage()
		for _, variant := range []struct {
			name   string
			system string
		}{
			{"v12", coachCorePromptV12Archive},
			{"v14", GetCoachPrompt(sc.Persona.SkillLevel)},
		} {
			t.Run(sc.ID+"/"+variant.name, func(t *testing.T) {
				result, err := TextCoachCompletion(ctx, cfg, client, "v14-compare-"+variant.name, variant.system, userMsg)
				if err != nil {
					t.Fatalf("%s: %v", variant.name, err)
				}
				out, err := parseCoachStructuredOutput(result.Text, "v14-compare")
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				sc.Persona.VisionJSON = sc.VisionJSON
				score := ScoreCoachPersonalization(sc.Persona, out, true)
				rows = append(rows, row{
					id: sc.ID, prompt: variant.name,
					vision: score.VisionDetailCount, generic: score.HasGenericPhrases,
					hist: score.HasHistoryCallback, preview: truncateRunes(out.SituationAnalysis, 120),
				})
				t.Logf("%-18s | %-6s | %6d | %7v | %4v | %q",
					sc.ID, variant.name, score.VisionDetailCount, score.HasGenericPhrases,
					score.HasHistoryCallback, truncateRunes(out.SituationAnalysis, 100))
			})
		}
	}

	t.Log("")
	t.Log("### Aggregate")
	v12Vision, v14Vision := 0, 0
	v12Generic, v14Generic := 0, 0
	v12Hist, v14Hist := 0, 0
	v12N, v14N := 0, 0
	for _, r := range rows {
		switch r.prompt {
		case "v12":
			v12Vision += r.vision
			if r.generic {
				v12Generic++
			}
			if r.hist {
				v12Hist++
			}
			v12N++
		case "v14":
			v14Vision += r.vision
			if r.generic {
				v14Generic++
			}
			if r.hist {
				v14Hist++
			}
			v14N++
		}
	}
	if v12N > 0 && v14N > 0 {
		t.Logf("Avg vision details: v12=%.1f v14=%.1f (target ≥%d)",
			float64(v12Vision)/float64(v12N), float64(v14Vision)/float64(v14N), MinVisionDetailCitations)
		t.Logf("Generic phrase hits: v12=%d/%d v14=%d/%d", v12Generic, v12N, v14Generic, v14N)
		t.Logf("History callbacks: v12=%d/%d v14=%d/%d", v12Hist, v12N, v14Hist, v14N)
		if float64(v14Vision)/float64(v14N) < float64(MinVisionDetailCitations) {
			t.Errorf("v14 avg vision details below target %d", MinVisionDetailCitations)
		}
	}
	t.Log(sep)
}

func httpClientForCoachTests() *http.Client {
	return &http.Client{Timeout: 4 * time.Minute}
}
