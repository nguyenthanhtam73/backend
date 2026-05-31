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

// coachCorePromptV18Archive — pre-v19 system prompt for A/B live comparison only.
const coachCorePromptV18Archive = `Bạn là DaDiary AI Skincare Coach — người bạn thân thiết, luôn quan sát kỹ ảnh da và khích lệ user một cách chân thành, gần gũi.

Hôm nay mình đã nhìn kỹ ảnh và ghi chú của bạn rồi. Mình sẽ nói thật lòng những gì mình thấy, vừa cụ thể vừa động viên bạn nhé.

## Giọng (BẮT BUỘC — ấm, không lạnh)
- Như bạn thân nhắn tin · khen chân thành · closing động viên nhẹ.
- ≥4 chi tiết ảnh · mở "Mình thấy hôm nay…" / "Hôm nay da bạn…".
- So với lần trước khi có ## Recent SkinChecks.

## USER_MEMORY + JSON output per schema.`

func coachPromptV18Archive(skillLevel string) string {
	core := coachCorePromptV18Archive
	if strings.EqualFold(strings.TrimSpace(skillLevel), "beginner") {
		return core + "\n\n## BEGINNER\nTừ dễ · ≥4 chi tiết ảnh."
	}
	return core + "\n\n## INTERMEDIATE\n≥4 chi tiết · gợi ý cụ thể."
}

type coachCompareRow struct {
	id, prompt       string
	vision           int
	naturalness      float64
	emotional        float64
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
				result, err := TextCoachCompletion(ctx, cfg, client, "v19-compare-"+variant.name, system, userMsg)
				if err != nil {
					t.Fatalf("%s: %v", variant.name, err)
				}
				out, err := parseCoachStructuredOutput(result.Text, "v19-compare")
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				sc.Persona.VisionJSON = sc.VisionJSON
				pers := ScoreCoachPersonalization(sc.Persona, out, true)
				nat := ScoreCoachNaturalness(out)
				rows = append(rows, coachCompareRow{
					id: sc.ID, prompt: variant.name,
					vision: pers.VisionDetailCount, naturalness: nat.NaturalnessScore,
					emotional: nat.EmotionalScore,
					opener: nat.HasConversationalOpener, enc: nat.HasWarmEncouragement,
					report: pers.HasReportLikeTone, generic: pers.HasGenericPhrases,
					hist: pers.HasHistoryCallback,
				})
				t.Logf("%-22s | %-6s | %3d | %.2f | %.2f | %4v | %4v | %4v | %4v | %4v | %q",
					sc.ID, variant.name, pers.VisionDetailCount, nat.NaturalnessScore, nat.EmotionalScore,
					nat.HasConversationalOpener, nat.HasWarmEncouragement, pers.HasReportLikeTone,
					pers.HasGenericPhrases, pers.HasHistoryCallback, truncateRunes(out.SituationAnalysis, 85))
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
		nat, emo                                 float64
		opn, enc, rep, gen, hist                 int
	}
	byPrompt := map[string]*agg{left: {}, right: {}}
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
	for _, label := range []string{left, right} {
		a := byPrompt[label]
		if a.n == 0 {
			continue
		}
		t.Logf("[%s] Avg vision: %.1f | naturalness: %.2f | emotional: %.2f | opener: %d/%d | warm-enc: %d/%d | report: %d/%d | generic: %d/%d | history: %d/%d",
			label,
			float64(a.vis)/float64(a.n), a.nat/float64(a.n), a.emo/float64(a.n),
			a.opn, a.n, a.enc, a.n, a.rep, a.n, a.gen, a.n, a.hist, a.n)
	}
	l, r := byPrompt[left], byPrompt[right]
	if l.n > 0 && r.n > 0 {
		t.Logf("Delta %s-%s: vision %+.1f | naturalness %+.2f | emotional %+.2f",
			right, left,
			float64(r.vis)/float64(r.n)-float64(l.vis)/float64(l.n),
			r.nat/float64(r.n)-l.nat/float64(l.n),
			r.emo/float64(r.n)-l.emo/float64(l.n))
	}
}

// TestCoachV19_UserPhotoLiveCompare runs 2 user cheek photo scenarios (v18 vs v19).
//
// Run: go test ./internal/service/ai/... -run TestCoachV19_UserPhotoLiveCompare -v -count=1 -timeout 60m
func TestCoachV19_UserPhotoLiveCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live v19 user photo compare in short mode")
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
	t.Logf("User photo A/B — v18 archive vs v19 (current) | scenarios=%d (%s, %s)",
		len(scenarios), scenarios[0].ID, scenarios[1].ID)
	t.Logf("v19 prompt chars (intermediate): %d", len(GetCoachPrompt("intermediate")))
	t.Log(sep)

	variants := []struct {
		name   string
		system func(string) string
	}{
		{"v18", coachPromptV18Archive},
		{"v19", GetCoachPrompt},
	}
	rows := runCoachCompare(t, ctx, cfg, client, scenarios, variants)

	t.Log("")
	t.Log("### Aggregate — user cheek photos (v18 vs v19)")
	logCoachCompareAggregate(t, rows, "v18", "v19")

	v19Rows := 0
	v19Vis := 0
	for _, r := range rows {
		if r.prompt != "v19" {
			continue
		}
		v19Rows++
		v19Vis += r.vision
		if r.vision < MinVisionDetailCitations {
			t.Errorf("%s: v19 vision details %d below target %d", r.id, r.vision, MinVisionDetailCitations)
		}
		if !r.hist {
			t.Errorf("%s: v19 missing history callback", r.id)
		}
		if !r.opener {
			t.Errorf("%s: v19 missing specific conversational opener", r.id)
		}
	}
	if v19Rows > 0 && float64(v19Vis)/float64(v19Rows) < float64(MinVisionDetailCitations) {
		t.Errorf("v19 avg vision details below target %d", MinVisionDetailCitations)
	}
	t.Log(sep)
}

func httpClientForCoachTests() *http.Client {
	return &http.Client{Timeout: 4 * time.Minute}
}
