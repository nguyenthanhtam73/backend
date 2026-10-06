package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestSanitizeAndroidVoiceText(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Đm da mày hôm nay vl.", "Da bạn hôm nay."},
		{"Đm da mày…", "Da bạn…"},
		{"ĐM, mày ơi.", "Bạn ơi."},
		{"Trên ảnh tao thấy vùng má.", "Trên ảnh mình thấy vùng má."},
		{"Tao thấy ổn.", "Mình thấy ổn."},
		{"Mày thấy má trái.", "Bạn thấy má trái."},
		{"Lông mày đậm, chân mày rõ.", "Lông mày đậm, chân mày rõ."},
		{"Má mày đỏ vl, đừng nặn.", "Má bạn đỏ, đừng nặn."},
		{"Má  mày   đỏ", "Má bạn đỏ"},
		{"Xong vl, rồi.", "Xong, rồi."},
		{"Vì vl da đang đỏ", "Vì da đang đỏ"},
		{"VCL", ""},
		{"đéo hiểu", "không hiểu"},
		{"đếch hiểu", "không hiểu"},
		{"Đéo hiểu", "Không hiểu"},
		{"đéo được nặn", "không được nặn"},
		{"Da mày đéo ổn", "Da bạn không ổn"},
		{"Da mày đéo ổn đâu", "Da bạn không ổn đâu"},
		{"Đếch được nặn", "Không được nặn"},
		{"level và da ổn", "level và da ổn"},
		{"", ""},
		{"Không có gì lạ.", "Không có gì lạ."},
		{"  đm  ", ""},
		{"địt", ""},
		{"đệt", ""},
		{"vkl", ""},
		{"vãi", ""},
		{"vãi là", ""},
		{"dm", ""},
		{"DM da mày.", "Da bạn."},
		{"nói vkl đi", "nói đi"},
		{"không địt được", "không được"},
		{"vãi là đi", "Đi"},
		{"vãi, là ổn", "Là ổn"},
		{"địtcon", "địtcon"},
		{"admin nói", "admin nói"},
		{"Da ổn. đm mày thấy.", "Da ổn. Bạn thấy."},
	}
	for _, tc := range cases {
		got := SanitizeAndroidVoiceText(tc.in)
		if got != tc.want {
			t.Errorf("in %q\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeAndroidCoachOutput_UserFacingOnly(t *testing.T) {
	out := &CoachStructuredOutput{
		SituationAnalysis: "Đm da mày hôm nay.",
		SummaryNotes:      "Mai tao check mày.",
		Strengths:         []string{"Chụp rõ vl"},
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Tối: dịu má mày", Why: "Vì vl da đang đỏ"}},
		CareSuggestions: []CoachCareSuggestion{{
			Step: "Rửa mặt", Why: "Má mày đỏ", SafetyNote: "Đừng nặn đm",
		}},
		RoutineHints:    []string{"Tối: thoa ẩm cho mày"},
		AvoidOrPatch:    []string{"Đừng chồng BHA vl"},
		SafetyReminders: []string{"Đi khám nếu sưng đéo đỡ"},
		ProductSuggestions: []dto.ProductSuggestion{{
			ProductName:   "Tao Toner",
			Brand:         "VL Lab",
			AffiliateLink: "https://example.test/vl",
			Reason:        "Hợp má mày",
		}},
	}
	if !SanitizeAndroidCoachOutput(out) {
		t.Fatal("expected a rewrite")
	}
	if out.SituationAnalysis != "Da bạn hôm nay." {
		t.Fatalf("situation %q", out.SituationAnalysis)
	}
	if out.SafetyReminders[0] != "Đi khám nếu sưng không đỡ" {
		t.Fatalf("reminder %q", out.SafetyReminders[0])
	}
	if out.SummaryNotes != "Mai mình check bạn." {
		t.Fatalf("summary %q", out.SummaryNotes)
	}
	if out.Strengths[0] != "Chụp rõ" {
		t.Fatalf("strength %q", out.Strengths[0])
	}
	if out.Improvements[0].Tip != "Tối: dịu má bạn" || out.Improvements[0].Why != "Vì da đang đỏ" {
		t.Fatalf("improvement %+v", out.Improvements[0])
	}
	if out.CareSuggestions[0].Why != "Má bạn đỏ" || out.CareSuggestions[0].SafetyNote != "Đừng nặn" {
		t.Fatalf("care %+v", out.CareSuggestions[0])
	}
	if out.ProductSuggestions[0].ProductName != "Tao Toner" || out.ProductSuggestions[0].Brand != "VL Lab" {
		t.Fatalf("catalog fields were rewritten: %+v", out.ProductSuggestions[0])
	}
	if out.ProductSuggestions[0].Reason != "Hợp má bạn" {
		t.Fatalf("reason %q", out.ProductSuggestions[0].Reason)
	}
	if out.ProductSuggestions[0].AffiliateLink != "https://example.test/vl" {
		t.Fatal("link rewritten")
	}
	if SanitizeAndroidCoachOutput(out) {
		t.Fatal("second pass should be a no-op")
	}
}

func TestSanitizeAndroidVoice_EyebrowMay(t *testing.T) {
	// Previous word keeps "mày" as eyebrow.
	prev := []string{"kẻ", "tỉa", "chì", "đầu", "đuôi", "cung", "hai", "giữa", "phun", "xăm", "vẽ", "sợi", "lông", "chân"}
	for _, w := range prev {
		in := w + " mày"
		t.Run("prev "+w, func(t *testing.T) {
			if got := SanitizeAndroidVoiceText(in); got != in {
				t.Fatalf("got %q", got)
			}
		})
		upper := strings.ToUpper(w) + " MÀY"
		t.Run("prev upper "+w, func(t *testing.T) {
			if got := SanitizeAndroidVoiceText(upper); got != upper {
				t.Fatalf("got %q", got)
			}
		})
	}
	// Next word keeps "mày" as eyebrow.
	next := []string{"râu", "mắt", "ngài"}
	for _, w := range next {
		in := "mày " + w
		t.Run("next "+w, func(t *testing.T) {
			if got := SanitizeAndroidVoiceText(in); got != in {
				t.Fatalf("got %q", got)
			}
		})
	}
	if got := SanitizeAndroidVoiceText("mụn giữa hai mày"); got != "mụn giữa hai mày" {
		t.Fatalf("got %q", got)
	}
	// A following word outside the eyebrow list is still the pronoun.
	if got := SanitizeAndroidVoiceText("mày hai"); got != "bạn hai" {
		t.Fatalf("pronoun got %q", got)
	}
}

func TestPipelineModelVersion_VoiceSuffix(t *testing.T) {
	const web = "pipeline=hybrid|vision=gpt-4o(ok)|coach=claude-sonnet-4-6(anthropic)"
	cases := []struct {
		kind string
		want string
	}{
		{"", web},
		{"web", web},
		{"Android", web},
		{"android", web + "+android"},
		{"  android  ", web + "+android"},
	}
	for _, tc := range cases {
		got := PipelineModelVersion("gpt-4o", "ok", "claude-sonnet-4-6", "anthropic", false, tc.kind)
		if got != tc.want {
			t.Errorf("kind %q\n got %q\nwant %q", tc.kind, got, tc.want)
		}
		if len(strings.Split(got, "|")) != 3 {
			t.Errorf("kind %q segments %q", tc.kind, got)
		}
	}
	const fallback = "pipeline=hybrid|vision=gpt-4o(ok)|coach=gpt-4o(openai,fallback)"
	if got := PipelineModelVersion("gpt-4o", "ok", "gpt-4o", "openai", true, "web"); got != fallback {
		t.Fatalf("web fallback %q", got)
	}
	if got := PipelineModelVersion("gpt-4o", "ok", "gpt-4o", "openai", true, "android"); got != fallback+"+android" {
		t.Fatalf("android fallback %q", got)
	}
	if CoachDailyPromptVersion != 28 {
		t.Fatalf("android voice must not bump prompt version, got %d", CoachDailyPromptVersion)
	}
}
