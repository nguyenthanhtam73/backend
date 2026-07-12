package ai

import (
	"strings"
	"testing"
)

func TestCoachPrompt_v21_BucaToneRules(t *testing.T) {
	for _, skill := range []string{"beginner", "intermediate"} {
		t.Run(skill, func(t *testing.T) {
			p := GetCoachPrompt(skill)
			mustContain(t, p, "bựa bựa")
			mustContain(t, p, "xéo xắt")
			mustContain(t, p, "da hỗn hợp")
			mustContain(t, p, "da dễ nổi mụn")
			mustContain(t, p, "≥3–4 chi tiết cụ thể")
			mustContain(t, p, "Mày thấy hôm nay")
			mustContain(t, p, "Có … nốt mụn")
			mustContain(t, p, "sản phẩm nhẹ nhàng")
			mustContain(t, p, "troll nhẹ nhàng")
			if skill == "beginner" {
				mustContain(t, p, "Bớt bựa")
			}
		})
	}
}

func TestCoachPromptVersion_v23(t *testing.T) {
	if CoachDailyPromptVersion != 23 {
		t.Fatalf("expected CoachDailyPromptVersion == 23, got %d", CoachDailyPromptVersion)
	}
}

// TestCoachPrompt_v22_BrevityRules pins the tightened output limits so a future
// prompt tweak can't silently undo the latency-driven brevity constraints.
func TestCoachPrompt_v22_BrevityRules(t *testing.T) {
	p := GetCoachPrompt("intermediate")
	mustContain(t, p, "BREVITY")
	mustContain(t, p, "improvements 2–3")
	mustContain(t, p, "routine_hints 3–4")
}

// TestCoachPrompt_v23_PlainLanguageRules pins the v23 plain-language section so the
// English-jargon ban + Vietnamese translations can't be silently dropped later.
func TestCoachPrompt_v23_PlainLanguageRules(t *testing.T) {
	for _, skill := range []string{"beginner", "intermediate"} {
		p := GetCoachPrompt(skill)
		mustContain(t, p, "Quy tắc ngôn ngữ")
		mustContain(t, p, "jawline")
		mustContain(t, p, "vùng hàm")
		mustContain(t, p, "lớp bảo vệ da")
		mustContain(t, p, "bề mặt da")
	}
}

func TestMaxCoachValidationRetries_v20(t *testing.T) {
	if MaxCoachValidationRetries != 0 {
		t.Fatalf("expected MaxCoachValidationRetries == 0, got %d", MaxCoachValidationRetries)
	}
}

func TestMinVisionDetailCitations_v20(t *testing.T) {
	if MinVisionDetailCitations != 4 {
		t.Fatalf("expected MinVisionDetailCitations == 4, got %d", MinVisionDetailCitations)
	}
}

func TestScoreCoachNaturalness(t *testing.T) {
	natural := &CoachStructuredOutput{
		Strengths:         []string{"Bạn chụp cận vùng má rất rõ — bạn đang làm khá tốt rồi đó."},
		SituationAnalysis: "Mày thấy hôm nay vùng má trái có lỗ chân lông to, 2 chấm thâm nhỏ, da hồng nhẹ và texture sần — so với lần trước bạn cũng ghi má hồng.",
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Tối: rửa mặt dịu vùng má trái, thoa kem dưỡng ẩm quanh gò má."}},
		SummaryNotes: "Mai chụp cùng góc cận má nhé — mình tin bạn sẽ thấy da dịu hơn.",
	}
	score := ScoreCoachNaturalness(natural)
	if !score.HasConversationalOpener || !outputHasRequiredVisionOpener(natural) {
		t.Fatal("expected required vision opener")
	}
}

func TestScoreCoachBucaTone(t *testing.T) {
	buca := &CoachStructuredOutput{
		Strengths:         []string{"Chụp ảnh rõ vl, cố lên con — đáng khen."},
		SituationAnalysis: "Mày thấy hôm nay T-zone bóng dầu, cằm 4 nốt đỏ, lỗ chân lông to mũi — trông hơi thảm nhưng đỡ tệ.",
		SummaryNotes:      "Mai chụp lại nhé con ơi, mình tin mày.",
	}
	res := ScoreCoachBucaTone(buca)
	if res.MarkerCount < 3 {
		t.Fatalf("expected ≥3 buca markers, got %d (%v)", res.MarkerCount, res.HitMarkers)
	}
}

func TestOutputHasBannedGenericLabels(t *testing.T) {
	for _, phrase := range []string{
		"da hỗn hợp",
		"da dễ nổi mụn",
		"dễ nổi mụn",
	} {
		if !outputHasBannedGenericLabels(phrase) {
			t.Fatalf("expected banned phrase detection for %q", phrase)
		}
	}
	vague := &CoachStructuredOutput{
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Dùng sản phẩm nhẹ nhàng thôi."}},
	}
	if !outputHasVagueTipPhrases(vague) {
		t.Fatal("expected vague tip detection for sản phẩm nhẹ nhàng")
	}
}

func TestOutputHasRequiredVisionOpener(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"Mày thấy hôm nay má trái hơi đỏ.", true},
		{"Đm da mày hôm nay T-zone bóng dầu vl.", true},
		{"Mình thấy hôm nay má trái hơi đỏ.", true},
		{"Trên ảnh mình thấy vùng má gần tai sần nhẹ.", true},
		{"Có 3 nốt mụn ở cằm.", true},
		{"Da bạn hôm nay ổn.", false},
	}
	for _, tc := range cases {
		if got := outputHasRequiredVisionOpenerText(strings.ToLower(tc.text)); got != tc.want {
			t.Fatalf("opener %q: got %v want %v", tc.text, got, tc.want)
		}
	}
}

func TestCoachTurnChecklist_Vision(t *testing.T) {
	got := coachTurnChecklist("USER_MEMORY\n(no saved memory yet)", true)
	mustContain(t, got, "≥4–5 photo details")
	mustContain(t, got, "da hỗn hợp")
	// With MaxCoachValidationRetries == 0 the coach's first output is final, so the
	// checklist tells the model there is no second chance instead of promising retries.
	mustContain(t, got, "no second chance")
}

func TestUserPhotoCoachScenarios_Fixtures(t *testing.T) {
	scenarios := UserPhotoCoachScenarios()
	if len(scenarios) != 2 {
		t.Fatalf("want 2 user photo scenarios, got %d", len(scenarios))
	}
	for _, s := range scenarios {
		t.Run(s.ID, func(t *testing.T) {
			msg := s.CoachUserMessage()
			mustContain(t, msg, "≥4–5 photo details")
			mustContain(t, msg, "da hỗn hợp")
		})
	}
}

func TestNeedsCoachOutputRetry(t *testing.T) {
	vision := `{"visible_observations":["left cheek enlarged pores","2 brown PIH spots on cheek","mild pink cheek tone","rough cheek texture"]}`
	ctx := "## Recent SkinChecks\n- 2026-05-27 oily"
	good := &CoachStructuredOutput{
		Strengths:         []string{"Bạn chụp ảnh rất rõ — đáng khen, bạn đang làm khá tốt rồi đó."},
		SituationAnalysis: "Mày thấy hôm nay left cheek enlarged pores, 2 brown PIH spots on cheek, mild pink cheek tone và rough cheek texture — so với lần trước bạn cũng ghi má hồng.",
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Tối: rửa mặt dịu vùng má trái, thoa kem dưỡng ẩm quanh gò má."}},
		SummaryNotes: "Mai chụp lại cùng góc nhé — cố lên con, mình tin bạn.",
	}
	if needsCoachOutputRetry(vision, ctx, good) {
		t.Fatal("good output should pass coach validation")
	}
	vague := &CoachStructuredOutput{
		Strengths:         []string{"Bạn đang làm tốt lắm."},
		SituationAnalysis: "Da hỗn hợp dễ nổi mụn hôm nay.",
		SummaryNotes:      "Mai chụp lại nhé — mình tin bạn.",
	}
	if !needsCoachOutputRetry(vision, ctx, vague) {
		t.Fatal("generic combo-skin label should need retry")
	}
}

func TestCoachOutputRetryPrompt(t *testing.T) {
	got := coachOutputRetryPrompt(`{"visible_observations":["x"]}`, "## Recent SkinChecks\nfoo", 1)
	// The "/N" total tracks MaxCoachValidationRetries (now 0), so assert only the
	// stable attempt prefix rather than a hard-coded total.
	mustContain(t, got, "attempt 1/")
	mustContain(t, got, "da hỗn hợp")
	mustContain(t, got, "So với lần trước")
}
