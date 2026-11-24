package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestAndroidVoiceGuard_DoesNotRunWebPolish(t *testing.T) {
	const in = "Hôm nay da bạn ổn…"
	out := &CoachStructuredOutput{SituationAnalysis: in}
	if applyCoachVoiceGuard(out, "android", webQA{WorseThanLast: true}) {
		t.Fatal("android sanitizer reported a change")
	}
	if out.SituationAnalysis != in {
		t.Fatalf("got %q", out.SituationAnalysis)
	}
}

func TestMayIsNotDeleted(t *testing.T) {
	for _, in := range []string{"Mày ơi…", "Tao với mày cùng theo dõi"} {
		out := &CoachStructuredOutput{SituationAnalysis: in}
		polishWebCoachScreen(out)
		if out.SituationAnalysis != in {
			t.Fatalf("%q became %q", in, out.SituationAnalysis)
		}
	}
}

func TestJudgingPhrasesStay(t *testing.T) {
	for _, in := range []string{"Da trông mệt hơn", "Mắt trông mệt", "Nhớ rửa sạch"} {
		out := &CoachStructuredOutput{SituationAnalysis: in}
		polishWebCoachScreen(out)
		if out.SituationAnalysis != in {
			t.Fatalf("%q became %q", in, out.SituationAnalysis)
		}
	}
}

func TestSmoothSkinNegationStays(t *testing.T) {
	const in = "Da mịn màng, không có mụn ẩn"
	out := &CoachStructuredOutput{SituationAnalysis: in}
	polishWebCoachScreen(out)
	if out.SituationAnalysis != in {
		t.Fatalf("got %q", out.SituationAnalysis)
	}
	for _, sentence := range []string{
		"Da mịn, chưa thấy mụn ẩn",
		"không thấy mụn ẩn rõ",
		"không phải mụn ẩn",
	} {
		got := softenSmoothSkinClaim(sentence)
		if got != sentence {
			t.Fatalf("%q became %q", sentence, got)
		}
	}
	positive := softenSmoothSkinClaim("Da mịn màng, đây là mụn ẩn.")
	if strings.Contains(positive, "mụn ẩn") || !strings.Contains(positive, "có vài nốt nhỏ, chưa chắc") {
		t.Fatalf("positive claim stayed %q", positive)
	}
}

func TestImprovementIsNotWorseSkin(t *testing.T) {
	const in = "Da ẩm hơn lần trước"
	out := &CoachStructuredOutput{SituationAnalysis: in}
	polishWebCoachScreen(out)
	if out.SituationAnalysis != in || strings.Contains(out.SituationAnalysis, "Không sao đâu") {
		t.Fatalf("got %q", out.SituationAnalysis)
	}
}

func TestEnglishFilterSkipsProductNames(t *testing.T) {
	for _, in := range []string{"Klairs Redness Solutions", "Red Cica", "Red Ginseng"} {
		if got := ToVietnameseSkinWords(in); got != in {
			t.Fatalf("%q became %q", in, got)
		}
		out := &CoachStructuredOutput{ProductSuggestions: []dto.ProductSuggestion{{Reason: in}}}
		polishWebCoachScreen(out)
		if out.ProductSuggestions[0].Reason != in {
			t.Fatalf("product %q became %q", in, out.ProductSuggestions[0].Reason)
		}
	}
}

func TestChungTaBecomesTaoVoiMay(t *testing.T) {
	if got := replaceChungTa("chúng ta"); got != "tao với mày" {
		t.Fatalf("chúng ta -> %q", got)
	}
	if got := replaceChungTa("chúng mình"); got != "tao với mày" || strings.Contains(got, "tớ") {
		t.Fatalf("chúng mình -> %q", got)
	}
	if got := replaceChungTa("Chúng mình làm từ từ"); got != "Tao với mày làm từ từ" {
		t.Fatalf("Chúng mình làm từ từ -> %q", got)
	}
}

func TestNoInjectedTaoSentence(t *testing.T) {
	const in = "Da đang lên vài nốt."
	out := &CoachStructuredOutput{SituationAnalysis: in}
	polishWebCoachScreen(out)
	if out.SituationAnalysis != in || strings.Contains(out.SituationAnalysis, "Tao thấy da hôm nay vậy đó.") {
		t.Fatalf("got %q", out.SituationAnalysis)
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
