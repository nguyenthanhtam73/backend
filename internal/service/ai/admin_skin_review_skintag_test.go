package ai

import (
	"strings"
	"testing"
)

func TestNormalizeAdminRegion_Neck(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"neck", "Neck", "cổ", "Cổ"} {
		if got := normalizeAdminRegion(in); got != "neck" {
			t.Fatalf("normalizeAdminRegion(%q)=%q want neck", in, got)
		}
	}
}

func TestAdminSkinReviewPrompt_SkinTagAndNeck(t *testing.T) {
	t.Parallel()
	p := AdminSkinReviewSystemPrompt()
	for _, needle := range []string{"mụn thịt", "ảnh vùng cổ", "Case 1e", "kích ứng nhẹ"} {
		if !strings.Contains(p, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
	if !strings.Contains(AdminSkinReviewJSONSchemaBlock, "neck") {
		t.Fatal("schema must allow neck region")
	}
}

func TestSuggestAnswerPrompt_SkinTagExample(t *testing.T) {
	t.Parallel()
	sys := adminSkinReviewSuggestAnswerSystemPrompt("vi")
	if !strings.Contains(sys, "mụn thịt") {
		t.Fatal("suggest system prompt missing mụn thịt frame")
	}
	msg := adminSkinReviewSuggestAnswerUserMessage("tẩy hoài không hết", nil, "vi")
	if !strings.Contains(msg, "Ví dụ 5") {
		t.Fatal("suggest user message missing skin-tag example 5")
	}
}

func TestAdminSkinReviewPrompt_PeriOralPigmentSplit(t *testing.T) {
	t.Parallel()
	p := AdminSkinReviewSystemPrompt()
	for _, needle := range []string{
		"Thâm / sẫm khóe miệng",
		"Viêm cấp sát mép môi",
		"Case 1f",
		"CẤM tuyệt đối",
		"viêm cấp sát mép miệng",
		"đúng kiểu thâm sau mụn",
		"chùm hạt đỏ sưng rõ",
	} {
		if !strings.Contains(p, needle) {
			t.Fatalf("prompt missing peri-oral split needle %q", needle)
		}
	}
	if strings.Contains(p, "có thể là thâm sau mụn") {
		t.Fatal("Case 1f must not hedge with “có thể là thâm sau mụn”")
	}
	sys := adminSkinReviewSuggestAnswerSystemPrompt("vi")
	if !strings.Contains(sys, "7a") || !strings.Contains(sys, "7b") {
		t.Fatal("suggest prompt must split 7a pigment vs 7b acute lip-edge")
	}
	msg := adminSkinReviewSuggestAnswerUserMessage("thâm 2 mép môi và dưới cằm", nil, "vi")
	if !strings.Contains(msg, "Ví dụ 6") {
		t.Fatal("suggest user message missing peri-oral thâm example 6")
	}
}
