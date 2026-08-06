package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestSoftenCheekLateralityProse(t *testing.T) {
	t.Parallel()
	in := "Má phải của mày đang đỏ. Má trái yên hơn."
	out := SoftenCheekLateralityProse(in)
	if strings.Contains(out, "má phải") || strings.Contains(out, "Má phải") {
		t.Fatalf("still has má phải: %q", out)
	}
	if strings.Contains(strings.ToLower(out), "má trái") {
		t.Fatalf("still has má trái: %q", out)
	}
	if !strings.Contains(out, "má gần tai") {
		t.Fatalf("expected má gần tai, got %q", out)
	}
}

func TestAlignAdminSkinAnalysisWithQuestion_SpotProductTips(t *testing.T) {
	t.Parallel()
	a := &dto.AdminSkinReviewAnalysis{
		Overview:   "Má phải của mày đang có cụm mụn viêm.",
		PhotoNotes: "Ảnh close-up má — thiếu trán, mũi, cằm.",
		PossibleCauses: []string{
			"Do dầu bít tắc làm bóng và kích ứng tại chỗ.",
			"Do tóc chạm má.",
		},
		SoothingTips: []string{
			"Không nặn ổ đang sưng đỏ.",
			"Rửa dịu, tạm nghỉ sản phẩm trị mụn mạnh đang dùng.",
			"Nếu ổ to, đau hoặc kéo dài thì nên khám chuyên khoa da.",
		},
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "cheeks", Concern: "pustules", Severity: "moderate", Note: "Má phải đang sưng."},
			{Region: "forehead", Concern: "not_visible", Severity: "mild", Note: "Không thấy trán trên ảnh — chụp đủ mặt mới nhận xét được."},
			{Region: "nose", Concern: "not_visible", Severity: "mild", Note: "Không thấy mũi trên ảnh — chụp đủ mặt mới nhận xét được."},
			{Region: "chin", Concern: "not_visible", Severity: "mild", Note: "Không thấy cằm trên ảnh — chụp đủ mặt mới nhận xét được."},
		},
	}
	q := "Như này thì xử lý sao ạa, em đang bôi chấm mụn nên nhìn hơi bóng ạ"
	if !AlignAdminSkinAnalysisWithQuestion(a, q, "vi") {
		t.Fatal("expected alignment changes")
	}
	if strings.Contains(a.Overview, "Má phải") || strings.Contains(a.Overview, "má phải") {
		t.Fatalf("close-up laterality not softened: %q", a.Overview)
	}
	for _, tip := range a.SoothingTips {
		if tipLooksPauseStrongProduct(tip) {
			t.Fatalf("pause-strong tip should be removed, got %q", tip)
		}
	}
	joinedCauses := strings.ToLower(strings.Join(a.PossibleCauses, " "))
	if !strings.Contains(joinedCauses, "kem") && !strings.Contains(joinedCauses, "chấm") {
		t.Fatalf("expected product-film cause, got %#v", a.PossibleCauses)
	}
}

func TestAdminSkinReviewSuggestAnswerUserMessage_OmitsTipsAndSoftens(t *testing.T) {
	t.Parallel()
	a := &dto.AdminSkinReviewAnalysis{
		Overview:               "Má phải của mày đang đỏ sưng.",
		SkinType:               "unclear",
		SkinTypeSeverity:       "mild",
		SkinTypeNote:           "Chỉ thấy má.",
		AdditionalObservations: "Thâm nông.",
		PhotoNotes:             "Close-up má phải — thiếu trán mũi cằm.",
		PossibleCauses:         []string{"Do dầu bít tắc."},
		SoothingTips:           []string{"Tạm nghỉ sản phẩm mạnh đang dùng."},
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "cheeks", Concern: "pustules", Severity: "moderate", Note: "Má phải có cụm viêm."},
			{Region: "forehead", Concern: "not_visible", Severity: "mild", Note: "Không thấy trán trên ảnh — chụp đủ mặt mới nhận xét được."},
			{Region: "nose", Concern: "not_visible", Severity: "mild", Note: "Không thấy mũi trên ảnh — chụp đủ mặt mới nhận xét được."},
			{Region: "chin", Concern: "not_visible", Severity: "mild", Note: "Không thấy cằm trên ảnh — chụp đủ mặt mới nhận xét được."},
		},
	}
	msg := adminSkinReviewSuggestAnswerUserMessage(
		"Anh chị giúp em sai ở bước nào, em da nhiều dầu",
		a,
		"vi",
	)
	if !strings.Contains(msg, "sai ở bước nào") {
		t.Fatal("user question missing from suggest payload")
	}
	if strings.Contains(msg, "possible_causes") || strings.Contains(msg, "soothing_tips") {
		t.Fatalf("suggest payload must omit causes/tips, got: %s", msg)
	}
	if strings.Contains(msg, "má phải") || strings.Contains(msg, "Má phải") {
		t.Fatalf("close-up suggest payload must soften laterality, got: %s", msg)
	}
	if !strings.Contains(msg, "má gần tai") {
		t.Fatalf("expected softened cheek wording, got: %s", msg)
	}
}

func TestAdminSkinReviewUserText_InjectsQuestion(t *testing.T) {
	t.Parallel()
	q := "em da nhiều dầu và đang bôi chấm mụn"
	msg := adminSkinReviewUserText("vi", false, q)
	if !strings.Contains(msg, "CONTEXT TỪ CÂU HỎI USER") {
		t.Fatal("missing question context header")
	}
	if !strings.Contains(msg, q) {
		t.Fatal("question not injected into analyze user text")
	}
	empty := adminSkinReviewUserText("vi", false, "")
	if strings.Contains(empty, "CONTEXT TỪ CÂU HỎI USER") {
		t.Fatal("empty question should not inject context block")
	}
}
