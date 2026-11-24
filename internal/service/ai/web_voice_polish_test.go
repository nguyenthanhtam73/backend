package ai

import (
	"strings"
	"testing"
)

func TestWebScreen_MayBudgetAndConsistentVoice(t *testing.T) {
	out := &CoachStructuredOutput{
		SituationAnalysis: "Da mày hôm nay mày thấy mày ổn, mày đừng lo, mày cứ thở.",
		SummaryNotes:      "Chúng ta sẽ xem lại ngày mai.",
		ConcernAlignment:  "Da mày vẫn da mày.",
	}
	if !polishWebCoachScreen(out) {
		t.Fatal("expected a rewrite")
	}
	blob := out.SituationAnalysis + "\n" + out.SummaryNotes + "\n" + out.ConcernAlignment
	if strings.Contains(blob, "chúng ta") || strings.Contains(strings.ToLower(blob), "ta sẽ") {
		t.Fatalf("plural we survived: %s", blob)
	}
	if strings.Count(strings.ToLower(blob), "mày") > 2 {
		t.Fatalf("more than 2 mày: %s", blob)
	}
	if !CoachProseHasTaoMay(blob) {
		t.Fatalf("screen lost tao/mày: %s", blob)
	}
	quiet := &CoachStructuredOutput{SituationAnalysis: "Da đang lên vài nốt."}
	polishWebCoachScreen(quiet)
	if !CoachProseHasTaoMay(quiet.SituationAnalysis) {
		t.Fatalf("worse-skin style empty voice stayed at 0: %q", quiet.SituationAnalysis)
	}
}

func TestToVietnameseSkinWords_LeakedZoneNote(t *testing.T) {
	got := ToVietnameseSkinWords("Trông giống red, swollen bump indicating mụn viêm")
	want := "Trông giống nốt đỏ sưng mụn viêm"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	raw := `{"zone_observations":[{"zone":"left_cheek","cue":"Trông giống red, swollen bump indicating mụn viêm","severity":"mild"}]}`
	out, changed := SanitizeCheckInVisionJSON(raw, "vi")
	if !changed || !strings.Contains(out, want) || strings.Contains(strings.ToLower(out), "swollen") {
		t.Fatalf("vision cue not translated: %s", out)
	}
}

func TestWebScreen_WorseSkinReassuresAndDropsJudgement(t *testing.T) {
	out := &CoachStructuredOutput{
		SituationAnalysis: "Da chưa sạch và trông mệt hơn lần trước. Không được rửa sạch đủ.",
	}
	polishWebCoachScreen(out)
	if !strings.HasPrefix(out.SituationAnalysis, "Không sao đâu, da có hôm lên hôm xuống.") {
		t.Fatalf("opening %q", out.SituationAnalysis)
	}
	low := strings.ToLower(out.SituationAnalysis)
	for _, banned := range []string{"da chưa sạch", "rửa sạch đủ", "không được rửa sạch", "trông mệt"} {
		if strings.Contains(low, banned) {
			t.Fatalf("judging %q still in %q", banned, out.SituationAnalysis)
		}
	}
}

func TestCapFollowUpQuestions_KeepsRetakeTips(t *testing.T) {
	in := append(RetakePhotoTips("vi"), "Sờ thấy cứng không?", "Đau không?", "Lên bao lâu rồi?")
	got := capFollowUpQuestions(in)
	questions := 0
	for _, q := range got {
		if strings.Contains(q, "?") {
			questions++
		}
	}
	if questions != 1 {
		t.Fatalf("questions %d in %#v", questions, got)
	}
	if len(got) != len(RetakePhotoTips("vi"))+1 {
		t.Fatalf("retake tips dropped: %#v", got)
	}
}

func TestGloss_FirstMentionOnly(t *testing.T) {
	out := &CoachStructuredOutput{
		SituationAnalysis: "Thử patch-test, rồi milia, acid mạnh và retinoid.",
		SummaryNotes:      "Nhắc lại milia và retinoid, không giải thích lần hai.",
	}
	polishWebCoachScreen(out)
	blob := out.SituationAnalysis + " " + out.SummaryNotes
	for _, gloss := range []string{
		"thử trước ở một vùng nhỏ",
		"hạt nhỏ trắng dưới da",
		"chất tẩy da chết mạnh",
		"chất giúp da thay mới, dễ gây khô rát",
	} {
		if strings.Count(blob, gloss) != 1 {
			t.Fatalf("gloss %q count %d in %s", gloss, strings.Count(blob, gloss), blob)
		}
	}
	if strings.Contains(blob, "((") || strings.Contains(blob, "(hạt nhỏ trắng dưới da (") {
		t.Fatalf("nested parens: %s", blob)
	}
	again := &CoachStructuredOutput{SituationAnalysis: "milia (hạt nhỏ trắng dưới da)"}
	polishWebCoachScreen(again)
	if strings.Count(again.SituationAnalysis, "hạt nhỏ trắng dưới da") != 1 {
		t.Fatalf("double gloss %q", again.SituationAnalysis)
	}
}

func TestTyposAndSmoothSkin(t *testing.T) {
	got := applyTypos("Da trông mịn mặn.")
	if got != "Da trông mịn màng." {
		t.Fatalf("typo %q", got)
	}
	out := &CoachStructuredOutput{SituationAnalysis: "Da mịn màng, đây là mụn ẩn."}
	polishWebCoachScreen(out)
	if strings.Contains(out.SituationAnalysis, "mụn ẩn") || !strings.Contains(out.SituationAnalysis, "có vài nốt nhỏ, chưa chắc") {
		t.Fatalf("smooth skin still concluded mụn ẩn: %q", out.SituationAnalysis)
	}
	bumps := polishOneSentence("Nốt tròn mịn, trông giống mụn ẩn.")
	if !strings.Contains(bumps, "mụn ẩn") {
		t.Fatalf("real bumps were softened: %q", bumps)
	}
}

func TestCheekSide_IsTheUsersSide(t *testing.T) {
	rules := VisionMorphologyRules()
	for _, needle := range []string{
		"bên trái của user",
		"bên phải của người xem",
		"left_cheek",
		"CẤM gắn trái/phải theo mép khung ảnh",
	} {
		if !strings.Contains(rules, needle) {
			t.Fatalf("vision rules missing %q", needle)
		}
	}
	if !strings.Contains(VisionObservationSystemPrompt(), rules) {
		t.Fatal("check-in vision prompt dropped the shared side rule")
	}
	web := GetCoachPrompt("intermediate")
	if !strings.Contains(web, "má bên trái của user") {
		t.Fatal("web coach prompt missing the user's-side cheek rule")
	}
}
