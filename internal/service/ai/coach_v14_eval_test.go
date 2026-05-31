package ai

import (
	"strings"
	"testing"
)

func TestCoachPrompt_v14_VisionRules(t *testing.T) {
	for _, skill := range []string{"beginner", "intermediate"} {
		t.Run(skill, func(t *testing.T) {
			p := GetCoachPrompt(skill)
			mustContain(t, p, "≥3 chi tiết cụ thể từ ảnh")
			mustContain(t, p, "Vision Observation")
			mustContain(t, p, "da bạn hơi khô")
			mustContain(t, p, "So sánh lịch sử")
			mustContain(t, p, "thâm sau mụn")
			mustContain(t, p, "USER_MEMORY")
		})
	}
}

func TestCoachPromptVersion_v14(t *testing.T) {
	if CoachDailyPromptVersion != 14 {
		t.Fatalf("expected CoachDailyPromptVersion == 14, got %d", CoachDailyPromptVersion)
	}
}

func TestCountVisionDetailCitations(t *testing.T) {
	vision := `{"visible_observations":["T-zone bóng dầu rõ","4 nốt mụn đỏ ở cằm","lỗ chân lông to ở mũi"],"texture_and_oil_cues":"","redness_or_discoloration_cues":""}`
	out := &CoachStructuredOutput{
		SituationAnalysis: "Hôm nay T-zone bóng dầu rõ, thấy 4 nốt mụn đỏ ở cằm và lỗ chân lông to ở mũi.",
		ConcernAlignment:  "Ảnh khớp với ghi chú da dầu vùng T.",
	}
	if n := CountVisionDetailCitations(vision, out); n < MinVisionDetailCitations {
		t.Fatalf("expected >= %d vision citations, got %d", MinVisionDetailCitations, n)
	}
}

func TestCountVisionDetailCitations_RegionFallback(t *testing.T) {
	vision := `{"visible_observations":["some generic note"],"texture_and_oil_cues":"","redness_or_discoloration_cues":""}`
	out := &CoachStructuredOutput{
		SituationAnalysis: "Trán và mũi bóng dầu, cằm có mụn đỏ, má trái hơi khô.",
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

func TestCoachTurnChecklist_Vision(t *testing.T) {
	got := coachTurnChecklist("USER_MEMORY\n(no saved memory yet)", true)
	mustContain(t, got, "≥3 photo-specific details")
}

func TestVisionCoachScenarios_Fixtures(t *testing.T) {
	scenarios := VisionCoachScenarios()
	if len(scenarios) != 4 {
		t.Fatalf("want 4 vision scenarios, got %d", len(scenarios))
	}
	for _, s := range scenarios {
		t.Run(s.ID, func(t *testing.T) {
			msg := s.CoachUserMessage()
			mustContain(t, msg, "VISION_SUMMARY_JSON")
			mustContain(t, msg, "visible_observations")
			mustContain(t, msg, "≥3 photo-specific details")
			if strings.TrimSpace(s.VisionJSON) == "" {
				t.Fatal("vision json required")
			}
		})
	}
}

func TestNeedsVisionDetailRetry(t *testing.T) {
	vision := `{"visible_observations":["T-zone oily","chin bumps","large pores"]}`
	good := &CoachStructuredOutput{SituationAnalysis: "T-zone oily with chin bumps and large pores on nose."}
	bad := &CoachStructuredOutput{SituationAnalysis: "Da bạn hơi khô hôm nay."}
	if needsVisionDetailRetry(vision, good) {
		t.Fatal("good output should not need retry")
	}
	if !needsVisionDetailRetry(vision, bad) {
		t.Fatal("vague output should need retry")
	}
}
