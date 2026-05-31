package ai

import (
	"strings"
	"testing"
)

func TestCoachPrompt_v17_WarmChatRules(t *testing.T) {
	for _, skill := range []string{"beginner", "intermediate"} {
		t.Run(skill, func(t *testing.T) {
			p := GetCoachPrompt(skill)
			mustContain(t, p, "người bạn thân thiết")
			mustContain(t, p, "≥4 chi tiết cụ thể")
			mustContain(t, p, "mình thấy")
			mustContain(t, p, "hôm nay da bạn")
			mustContain(t, p, "mình khuyên thật lòng nhé")
			mustContain(t, p, "bạn đang làm khá tốt rồi đó")
			mustContain(t, p, "USER_MEMORY")
			mustContain(t, p, "COACH_ACTION")
		})
	}
}

func TestCoachPromptVersion_v17(t *testing.T) {
	if CoachDailyPromptVersion != 17 {
		t.Fatalf("expected CoachDailyPromptVersion == 17, got %d", CoachDailyPromptVersion)
	}
}

func TestMinVisionDetailCitations_v17(t *testing.T) {
	if MinVisionDetailCitations != 4 {
		t.Fatalf("expected MinVisionDetailCitations == 4 for v17, got %d", MinVisionDetailCitations)
	}
}

func TestScoreCoachNaturalness(t *testing.T) {
	natural := &CoachStructuredOutput{
		Strengths:         []string{"Bạn chụp ảnh và ghi chú lại rồi — bạn đang làm khá tốt rồi đó."},
		SituationAnalysis: "Mình thấy hôm nay trán hơi bóng, cằm có vài nốt đỏ nhỏ, má trái hơi khô — giống vài hôm trước bạn cũng ghi vậy.",
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Tối nay mình khuyên thật lòng nhé — giữ routine nhẹ."}},
		SummaryNotes:      "Mai chụp cùng góc nhé — mình muốn xem cằm dịu lại chút nào.",
	}
	score := ScoreCoachNaturalness(natural)
	if !score.HasNaturalTone {
		t.Fatal("expected natural tone")
	}
	if !score.HasConversationalOpener {
		t.Fatal("expected conversational opener")
	}
	if !score.HasWarmOpening || !score.HasWarmClosing {
		t.Fatalf("expected warm open/close, got open=%v close=%v", score.HasWarmOpening, score.HasWarmClosing)
	}
	if !score.HasWarmEncouragement {
		t.Fatal("expected warm encouragement phrases")
	}
	if score.EmotionalScore < 0.5 {
		t.Fatalf("expected emotional score >= 0.5, got %.2f", score.EmotionalScore)
	}
	if score.HasReportLikeTone {
		t.Fatal("natural sample should not be report-like")
	}

	report := &CoachStructuredOutput{
		Strengths:         []string{"Good job."},
		SituationAnalysis: "Phân tích cho thấy tình trạng da hiện tại: 1. T-zone: oily 2. Má: dry",
		SummaryNotes:      "Continue routine.",
	}
	reportScore := ScoreCoachNaturalness(report)
	if !reportScore.HasReportLikeTone {
		t.Fatal("expected report-like detection")
	}
	if reportScore.HasConversationalOpener {
		t.Fatal("report sample should lack conversational opener")
	}
}

func TestCountVisionDetailCitations(t *testing.T) {
	vision := `{"visible_observations":["T-zone bóng dầu rõ","4 nốt mụn đỏ ở cằm","lỗ chân lông to ở mũi","má phải hơi xỉn"],"texture_and_oil_cues":"","redness_or_discoloration_cues":""}`
	out := &CoachStructuredOutput{
		SituationAnalysis: "Mình thấy hôm nay T-zone bóng dầu rõ, thấy 4 nốt mụn đỏ ở cằm, lỗ chân lông to ở mũi và má phải hơi xỉn.",
		ConcernAlignment:  "Ảnh khớp với ghi chú da dầu vùng T.",
	}
	if n := CountVisionDetailCitations(vision, out); n < MinVisionDetailCitations {
		t.Fatalf("expected >= %d vision citations, got %d", MinVisionDetailCitations, n)
	}
}

func TestCountVisionDetailCitations_RegionFallback(t *testing.T) {
	vision := `{"visible_observations":["some generic note"],"texture_and_oil_cues":"","redness_or_discoloration_cues":""}`
	out := &CoachStructuredOutput{
		SituationAnalysis: "Mình thấy trán và mũi bóng dầu, cằm có mụn đỏ, má trái hơi khô, má phải hơi xỉn.",
	}
	if n := CountVisionDetailCitations(vision, out); n < MinVisionDetailCitations {
		t.Fatalf("region fallback should reach %d, got %d", MinVisionDetailCitations, n)
	}
}

func TestOutputHasGenericSkinPhrases(t *testing.T) {
	if !outputHasGenericSkinPhrases("hôm nay da bạn hơi khô") {
		t.Fatal("expected generic phrase detection")
	}
	if !outputHasGenericSkinPhrases("da cần dưỡng ẩm thêm") {
		t.Fatal("expected da cần dưỡng ẩm detection")
	}
	if outputHasGenericSkinPhrases("má trái hơi khô, có vảy nhẹ quanh mũi") {
		t.Fatal("specific regional dryness should not trigger generic ban")
	}
}

func TestCoachTurnChecklist_Vision(t *testing.T) {
	got := coachTurnChecklist("USER_MEMORY\n(no saved memory yet)", true)
	mustContain(t, got, "≥4 photo details")
	mustContain(t, got, "mình thấy")
	mustContain(t, got, "NO lists/report tone")
}

func TestVisionCoachScenarios_Fixtures(t *testing.T) {
	scenarios := VisionCoachScenarios()
	if len(scenarios) != 6 {
		t.Fatalf("want 6 vision scenarios, got %d", len(scenarios))
	}
	for _, s := range scenarios {
		t.Run(s.ID, func(t *testing.T) {
			msg := s.CoachUserMessage()
			mustContain(t, msg, "VISION_SUMMARY_JSON")
			mustContain(t, msg, "visible_observations")
			mustContain(t, msg, "≥4 photo details")
			if strings.TrimSpace(s.VisionJSON) == "" {
				t.Fatal("vision json required")
			}
		})
	}
}

func TestNeedsVisionDetailRetry(t *testing.T) {
	vision := `{"visible_observations":["T-zone oily","chin bumps","large pores","dull cheek"]}`
	good := &CoachStructuredOutput{
		SituationAnalysis: "Mình thấy T-zone oily with chin bumps, large pores on nose and dull right cheek.",
		SummaryNotes:      "Mai chụp cùng góc nhé.",
		Strengths:         []string{"Bạn đang làm tốt lắm."},
	}
	bad := &CoachStructuredOutput{SituationAnalysis: "Da bạn hơi khô hôm nay."}
	if needsVisionDetailRetry(vision, good) {
		t.Fatal("good output should not need retry")
	}
	if !needsVisionDetailRetry(vision, bad) {
		t.Fatal("vague output should need retry")
	}
}

func TestNeedsNaturalToneRetry(t *testing.T) {
	out := &CoachStructuredOutput{
		SituationAnalysis: "Phân tích cho thấy: 1. T-zone dầu 2. Má khô",
		Strengths:         []string{"OK"},
		SummaryNotes:      "Done",
	}
	if !needsNaturalToneRetry(out) {
		t.Fatal("report-like output should need natural tone retry")
	}
}

func TestCoachPrompt_v17_Compactness(t *testing.T) {
	v17 := len(GetCoachPrompt("intermediate"))
	if v17 > 5500 {
		t.Fatalf("v17 prompt should be compact (<5500 chars), got %d", v17)
	}
}
