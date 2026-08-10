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

func TestAdminSkinReviewPrompt_NeckCrease(t *testing.T) {
	t.Parallel()
	p := AdminSkinReviewSystemPrompt()
	for _, needle := range []string{
		"Case 1g",
		"nếp gấp / nếp ngang cổ",
		"ảnh vùng cổ — không có mặt",
		"CẤM spam",
		"u tuyến giáp",
		"Giảm cúi điện thoại lâu",
	} {
		if !strings.Contains(p, needle) {
			t.Fatalf("prompt missing neck-crease needle %q", needle)
		}
	}
	if !strings.Contains(AdminSkinReviewJSONSchemaBlock, "nếp gấp") {
		t.Fatal("schema must mention neck creases")
	}
	sys := adminSkinReviewSuggestAnswerSystemPrompt("vi")
	if !strings.Contains(sys, "nếp gấp / nếp ngang cổ") {
		t.Fatal("suggest system prompt missing neck-crease frame")
	}
	msg := adminSkinReviewSuggestAnswerUserMessage("Em 22 tuổi cổ như thế này tips cải thiện ạ", nil, "vi")
	if !strings.Contains(msg, "Ví dụ 7") {
		t.Fatal("suggest user message missing neck-crease example 7")
	}
}

func TestSuggestAnswerPrompt_LargePoresFrame(t *testing.T) {
	t.Parallel()
	sys := adminSkinReviewSuggestAnswerSystemPrompt("vi")
	for _, needle := range []string{
		"lỗ chân lông to",
		"trông đỡ to",
		"se khít",
		"acid nhẹ kiểu BHA",
		"không se khít hẳn",
	} {
		if !strings.Contains(sys, needle) {
			t.Fatalf("suggest system prompt missing large-pores needle %q", needle)
		}
	}
	msg := adminSkinReviewSuggestAnswerUserMessage("làm thế nào để lỗ chân lông bớt to đây cm ơii", nil, "vi")
	if !strings.Contains(msg, "Ví dụ 8") {
		t.Fatal("suggest user message missing large-pores example 8")
	}
	if !strings.Contains(msg, "không se khít hẳn được") {
		t.Fatal("example 8 must include accurate se-khít caveat")
	}
	en := adminSkinReviewSuggestAnswerSystemPrompt("en")
	if !strings.Contains(en, "Large pores") {
		t.Fatal("EN suggest prompt missing Large pores frame")
	}
	p := AdminSkinReviewSystemPrompt()
	if !strings.Contains(p, "lỗ chân lông to / pores") {
		t.Fatal("analysis prompt missing pores soothing_tips case")
	}
	if !strings.Contains(p, "se khít / se lỗ chân lông") {
		t.Fatal("analysis prompt must ban se-khít wording for pores tips")
	}
}

func TestSuggestAnswerPrompt_RetinolInflamedFrame(t *testing.T) {
	t.Parallel()
	sys := adminSkinReviewSuggestAnswerSystemPrompt("vi")
	for _, needle := range []string{
		"Retinol / retinoid / Re",
		"mụn viêm đỏ sưng",
		"cách ngày hoặc bôi mỏng",
		"dưỡng ẩm + chống nắng",
		"dừng và làm dịu trước",
		"0.2–0.3%",
	} {
		if !strings.Contains(sys, needle) {
			t.Fatalf("suggest system prompt missing retinol needle %q", needle)
		}
	}
	msg := adminSkinReviewSuggestAnswerUserMessage("Re vision 0.25 ổn không cho da dầu mụn mới bắt đầu", nil, "vi")
	if !strings.Contains(msg, "Ví dụ 9") {
		t.Fatal("suggest user message missing retinol example 9")
	}
	if !strings.Contains(msg, "dùng rất nhẹ") || !strings.Contains(msg, "làm dịu trước") {
		t.Fatal("example 9 must keep cautious inflamed retinol frame")
	}
	en := adminSkinReviewSuggestAnswerSystemPrompt("en")
	if !strings.Contains(en, "Retinol / retinoid") {
		t.Fatal("EN suggest prompt missing Retinol frame")
	}
	if !strings.Contains(en, "stop and calm first") {
		t.Fatal("EN retinol frame must warn to stop and calm if worse")
	}
}

func TestSuggestAnswerPrompt_ClosedComedonesFrame(t *testing.T) {
	t.Parallel()
	sys := adminSkinReviewSuggestAnswerSystemPrompt("vi")
	for _, needle := range []string{
		"mụn ẩn / closed comedones",
		"Mụn ẩn thuần",
		"Nốt nhỏ + đỏ hồng rõ",
		"có phải kích ứng không",
		"tránh mọi thứ mạnh",
	} {
		if !strings.Contains(sys, needle) {
			t.Fatalf("suggest system prompt missing closed-comedones needle %q", needle)
		}
	}
	msg := adminSkinReviewSuggestAnswerUserMessage("có phải kích ứng không", nil, "vi")
	if !strings.Contains(msg, "Ví dụ 11") {
		t.Fatal("suggest user message missing red+bumps example 11")
	}
	if !strings.Contains(msg, "vừa mụn ẩn vừa đang kích ứng") {
		t.Fatal("example 11 must acknowledge irritation with closed comedones")
	}
	en := adminSkinReviewSuggestAnswerSystemPrompt("en")
	if !strings.Contains(en, "Tiny bumps + clear pink redness") {
		t.Fatal("EN suggest prompt missing red+bumps frame")
	}
	p := AdminSkinReviewSystemPrompt()
	for _, needle := range []string{
		"Mụn ẩn / closed comedones",
		"Nốt nhỏ + đỏ hồng rõ",
		"không thấy dấu hiệu viêm cấp",
		"chỉ mụn ẩn suông",
	} {
		if !strings.Contains(p, needle) {
			t.Fatalf("analysis prompt missing closed-comedones needle %q", needle)
		}
	}
	if !strings.Contains(AdminSkinReviewJSONSchemaBlock, "mụn ẩn") {
		t.Fatal("schema must mention mụn ẩn")
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
