package ai

import (
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestSanitizeAndroidVoiceText(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Đm da mày hôm nay vl.", "da bạn hôm nay."},
		{"Trên ảnh tao thấy vùng má.", "Trên ảnh mình thấy vùng má."},
		{"Tao thấy ổn.", "Mình thấy ổn."},
		{"Mày thấy má trái.", "Bạn thấy má trái."},
		{"Lông mày đậm, chân mày rõ.", "Lông mày đậm, chân mày rõ."},
		{"Má mày đỏ vl, đừng nặn.", "Má bạn đỏ, đừng nặn."},
		{"VCL", ""},
		{"đéo hiểu", "hiểu"},
		{"level và da ổn", "level và da ổn"},
		{"", ""},
		{"Không có gì lạ.", "Không có gì lạ."},
		{"  đm  ", ""},
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
	if out.SituationAnalysis != "da bạn hôm nay." {
		t.Fatalf("situation %q", out.SituationAnalysis)
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
