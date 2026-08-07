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
