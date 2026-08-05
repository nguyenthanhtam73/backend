package ai

import (
	"strings"
	"testing"
)

func TestAdminSkinEmptyOrRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		meta    adminSkinVisionMeta
		want    bool
	}{
		{"ok content", `{"overview":"x"}`, adminSkinVisionMeta{}, false},
		{"empty content", "", adminSkinVisionMeta{FinishReason: "stop"}, true},
		{"whitespace content", "   ", adminSkinVisionMeta{}, true},
		{"refusal with empty", "", adminSkinVisionMeta{Refusal: "I'm sorry, I can't assist with that."}, true},
		{"refusal with content", `{"a":1}`, adminSkinVisionMeta{Refusal: "can't assist"}, true},
		{"content apology refuse", "I'm sorry, I can't assist with that.", adminSkinVisionMeta{}, true},
		{"json ok not refuse", `{"overview":"Má đỏ nhẹ."}`, adminSkinVisionMeta{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := adminSkinEmptyOrRefused(tc.content, tc.meta); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestAcceptExpandedAdminSkinNote(t *testing.T) {
	t.Parallel()
	orig := "Má có vài nốt đỏ sưng. Màu đỏ nhẹ."
	good := "Má trông giống cụm nốt đỏ sưng. Khoảng vài hạt hơi nổi gần giữa má. Màu đỏ nhẹ, mức vừa. Chưa thấy đầu trắng rõ. Thường gặp khi dầu + bít tắc. Không chắc 100% chỉ từ một ảnh."
	if !acceptExpandedAdminSkinNote(orig, good) {
		t.Fatal("expected thicker clean rewrite to be accepted")
	}
	if acceptExpandedAdminSkinNote(orig, orig) {
		t.Fatal("same-length rewrite must be rejected")
	}
	if acceptExpandedAdminSkinNote(orig, good+" Nên dùng CeraVe.") {
		t.Fatal("brand rewrite must be rejected")
	}
	if acceptExpandedAdminSkinNote(orig, "Má có vài nốt. Nên thoa retinol nhẹ.") {
		t.Fatal("retinol rewrite must be rejected")
	}
	if acceptExpandedAdminSkinNote(orig, "Má có papules mức moderate. Có erythema.") {
		t.Fatal("jargon rewrite must be rejected")
	}
	if acceptExpandedAdminSkinNote(orig, "") {
		t.Fatal("empty rewrite must be rejected")
	}
	avoidAdvice := "Má trông giống cụm nốt đỏ sưng. Khoảng vài hạt hơi nổi gần giữa má. Màu đỏ nhẹ, mức vừa. Không nên dùng tay bẩn chạm mặt. Thường gặp khi dầu + bít tắc. Không chắc 100% chỉ từ một ảnh."
	if !acceptExpandedAdminSkinNote(orig, avoidAdvice) {
		t.Fatal("không nên dùng (avoid advice) must not false-positive ban")
	}
}

func TestAdminSkinReviewUserTextCompact(t *testing.T) {
	t.Parallel()
	full := adminSkinReviewUserText("vi", false)
	compact := adminSkinReviewUserText("vi", true)
	if len(compact) >= len(full) {
		t.Fatalf("compact user text should be shorter: compact=%d full=%d", len(compact), len(full))
	}
	for _, needle := range []string{"possible_causes", "soothing_tips", "Retry"} {
		if !strings.Contains(compact, needle) {
			t.Fatalf("compact text missing %q", needle)
		}
	}
	sys := AdminSkinReviewCompactSystemPrompt()
	for _, needle := range []string{"possible_causes", "soothing_tips", "Observations-first"} {
		if !strings.Contains(sys, needle) {
			t.Fatalf("compact system missing %q", needle)
		}
	}
}
