package ai

import (
	"strings"
	"testing"
)

func TestCoachPrompt_v19_SpecificVisionRules(t *testing.T) {
	for _, skill := range []string{"beginner", "intermediate"} {
		t.Run(skill, func(t *testing.T) {
			p := GetCoachPrompt(skill)
			mustContain(t, p, "zoom kỹ vào ảnh")
			mustContain(t, p, "≥4–5 chi tiết cụ thể")
			mustContain(t, p, "Mình thấy hôm nay")
			mustContain(t, p, "Trên ảnh mình thấy")
			mustContain(t, p, "Vùng … của bạn")
			mustContain(t, p, "sản phẩm nhẹ nhàng")
			mustContain(t, p, "So với lần trước")
			mustContain(t, p, "USER_MEMORY")
		})
	}
}

func TestCoachPromptVersion_v19(t *testing.T) {
	if CoachDailyPromptVersion != 19 {
		t.Fatalf("expected CoachDailyPromptVersion == 19, got %d", CoachDailyPromptVersion)
	}
}

func TestMinVisionDetailCitations_v19(t *testing.T) {
	if MinVisionDetailCitations != 4 {
		t.Fatalf("expected MinVisionDetailCitations == 4 for v19, got %d", MinVisionDetailCitations)
	}
}

func TestScoreCoachNaturalness(t *testing.T) {
	natural := &CoachStructuredOutput{
		Strengths:         []string{"Bạn chụp cận vùng má rất rõ — bạn đang làm khá tốt rồi đó."},
		SituationAnalysis: "Mình thấy hôm nay vùng má trái của bạn có lỗ chân lông hơi to, 2 chấm thâm nhỏ, da hơi hồng nhẹ và texture hơi sần — so với vài hôm trước bạn cũng ghi má hồng, có vẻ pattern quen.",
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Tối: rửa mặt dịu vùng má trái, thoa kem dưỡng ẩm dày hơn quanh gò má."}},
		SummaryNotes: "Mai chụp cùng góc cận má nhé — mình tin bạn sẽ thấy da dịu hơn chút.",
	}
	score := ScoreCoachNaturalness(natural)
	if !score.HasNaturalTone || !score.HasConversationalOpener {
		t.Fatal("expected natural tone with specific opener")
	}
	if !score.HasWarmEncouragement || score.EmotionalScore < 0.5 {
		t.Fatalf("expected warm emotional score, got %.2f enc=%v", score.EmotionalScore, score.HasWarmEncouragement)
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
		SituationAnalysis: "Trên ảnh mình thấy trán và mũi bóng dầu, cằm có mụn đỏ, má trái hơi khô, má phải hơi xỉn.",
	}
	if n := CountVisionDetailCitations(vision, out); n < MinVisionDetailCitations {
		t.Fatalf("region fallback should reach %d, got %d", MinVisionDetailCitations, n)
	}
}

func TestOutputHasGenericSkinPhrases(t *testing.T) {
	if !outputHasGenericSkinPhrases("hôm nay da bạn hơi khô") {
		t.Fatal("expected generic phrase detection")
	}
	if outputHasGenericSkinPhrases("má trái hơi khô, có vảy nhẹ quanh mũi") {
		t.Fatal("specific regional dryness should not trigger generic ban")
	}
}

func TestOutputHasVagueTipPhrases(t *testing.T) {
	vague := &CoachStructuredOutput{
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Hôm nay dùng sản phẩm nhẹ nhàng thôi nhé."}},
		Strengths:         []string{"OK"},
		SituationAnalysis: "Mình thấy hôm nay má đỏ.",
		SummaryNotes:      "Mai chụp lại nhé.",
	}
	if !outputHasVagueTipPhrases(vague) {
		t.Fatal("expected vague tip detection")
	}
	specific := &CoachStructuredOutput{
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Tối: rửa mặt dịu vùng má trái, thoa kem dưỡng ẩm quanh gò má."}},
	}
	if outputHasVagueTipPhrases(specific) {
		t.Fatal("specific tip should not trigger vague ban")
	}
}

func TestCoachTurnChecklist_Vision(t *testing.T) {
	got := coachTurnChecklist("USER_MEMORY\n(no saved memory yet)", true)
	mustContain(t, got, "≥4–5 photo details")
	mustContain(t, got, "Mình thấy hôm nay")
	mustContain(t, got, "sản phẩm nhẹ nhàng")
}

func TestCoachTurnChecklist_HistoryMandatory(t *testing.T) {
	got := coachTurnChecklist("## Recent SkinChecks\nfoo", true)
	mustContain(t, got, "HISTORY (MANDATORY)")
	mustContain(t, got, "TIPS (MANDATORY)")
}

func TestVisionCoachScenarios_Fixtures(t *testing.T) {
	if len(VisionCoachScenarios()) != 6 {
		t.Fatalf("want 6 vision scenarios")
	}
}

func TestUserPhotoCoachScenarios_Fixtures(t *testing.T) {
	scenarios := UserPhotoCoachScenarios()
	if len(scenarios) != 2 {
		t.Fatalf("want 2 user photo scenarios, got %d", len(scenarios))
	}
	for _, s := range scenarios {
		t.Run(s.ID, func(t *testing.T) {
			msg := s.CoachUserMessage()
			mustContain(t, msg, "VISION_SUMMARY_JSON")
			mustContain(t, msg, "≥4–5 photo details")
			mustContain(t, msg, "HISTORY (MANDATORY)")
			if strings.TrimSpace(s.VisionJSON) == "" {
				t.Fatal("vision json required")
			}
		})
	}
}

func TestNeedsVisionDetailRetry(t *testing.T) {
	vision := `{"visible_observations":["T-zone oily","chin bumps","large pores","dull cheek"]}`
	good := &CoachStructuredOutput{
		SituationAnalysis: "Mình thấy hôm nay T-zone oily with chin bumps, large pores on nose and dull right cheek.",
		SummaryNotes:      "Mai chụp cùng góc nhé — mình tin bạn sẽ thấy khác biệt.",
		Strengths:         []string{"Bạn đang làm khá tốt rồi đó."},
	}
	bad := &CoachStructuredOutput{SituationAnalysis: "Da bạn hơi khô hôm nay."}
	if needsVisionDetailRetry(vision, good) {
		t.Fatal("good output should not need retry")
	}
	if !needsVisionDetailRetry(vision, bad) {
		t.Fatal("vague output should need retry")
	}
}

func TestNeedsCoachOutputRetry(t *testing.T) {
	vision := `{"visible_observations":["left cheek enlarged pores","2 brown PIH spots on cheek","mild pink cheek tone","rough cheek texture"]}`
	ctx := "## Recent SkinChecks\n- 2026-05-27 oily"
	good := &CoachStructuredOutput{
		Strengths:         []string{"Bạn chụp ảnh rất rõ — đáng khen, bạn đang làm khá tốt rồi đó."},
		SituationAnalysis: "Mình thấy hôm nay left cheek enlarged pores, 2 brown PIH spots on cheek, mild pink cheek tone và rough cheek texture — so với lần trước bạn cũng ghi má hồng.",
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Tối: rửa mặt dịu vùng má trái, thoa kem dưỡng ẩm quanh gò má."}},
		SummaryNotes: "Mai chụp lại cùng góc nhé — mình tin bạn sẽ thấy da dịu hơn.",
	}
	if needsCoachOutputRetry(vision, ctx, good) {
		t.Fatal("good output should pass coach validation")
	}
	noHist := &CoachStructuredOutput{
		Strengths:         []string{"Bạn đang làm tốt lắm."},
		SituationAnalysis: "Mình thấy hôm nay má có lỗ chân lông to, thâm, đỏ, texture sần.",
		SummaryNotes:      "Mai chụp lại nhé — mình tin bạn.",
	}
	if !needsCoachOutputRetry(vision, ctx, noHist) {
		t.Fatal("missing history callback should need retry")
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
