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

func TestRewritePeriOralViemCapToTham_DoesNotMangleDenial(t *testing.T) {
	t.Parallel()
	in := "Đây là thâm/sắc tố quanh miệng — đúng kiểu thâm sau mụn. Không gọi viêm cấp sát mép khi không có sưng đau cấp. Thâm thì có."
	out := rewritePeriOralViemCapToTham(in)
	if strings.Contains(out, "Không gọi thâm") || strings.Contains(out, "không gọi thâm") {
		t.Fatalf("denial mangled into “không gọi thâm”: %q", out)
	}
	if strings.Contains(out, "Không gọi viêm cấp") {
		t.Fatalf("meta “không gọi viêm cấp” should be rewritten to plain denial: %q", out)
	}
	if !strings.Contains(out, "không phải ổ viêm đỏ sưng cấp") && !strings.Contains(out, "Không phải ổ viêm đỏ sưng cấp") {
		t.Fatalf("expected plain denial, got %q", out)
	}

	broken := "Đây là thâm/sắc tố quanh miệng. Không gọi thâm/sắc tố quanh miệng khi không có sưng đau cấp. Thâm thì có."
	fixed := rewritePeriOralViemCapToTham(broken)
	if strings.Contains(fixed, "Không gọi thâm") {
		t.Fatalf("should repair mangled line: %q", fixed)
	}
}

func TestAlignPeriOralThamVsAcute_SkipsPigmentFewShot(t *testing.T) {
	t.Parallel()
	a := &dto.AdminSkinReviewAnalysis{
		Overview: "Đây là thâm/sắc tố quanh miệng, không phải viêm cấp sát mép.",
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "chin", Concern: "pigmentation", Severity: "mild", Note: "Thâm nâu–xám. Không gọi viêm cấp sát mép khi không có sưng đau cấp."},
		},
	}
	before := a.AttentionAreas[0].Note
	if AlignAdminSkinAnalysisWithQuestion(a, "thâm 2 mép môi và dưới cằm", "vi") {
		// May still rewrite denial wording for clarity, but must not flip concern or invent acute tips removal wrongly.
	}
	if a.AttentionAreas[0].Concern != "pigmentation" {
		t.Fatalf("pigment concern must stay, got %q", a.AttentionAreas[0].Concern)
	}
	note := a.AttentionAreas[0].Note
	if strings.Contains(note, "Không gọi thâm") {
		t.Fatalf("mangled denial: before=%q after=%q", before, note)
	}
}

func TestAlignPeriOralThamVsAcute_WrongViemCapToPigment(t *testing.T) {
	t.Parallel()
	a := &dto.AdminSkinReviewAnalysis{
		Overview:   "Close-up khóe miệng. Đây là viêm cấp sát mép miệng.",
		PhotoNotes: "Crop quanh miệng.",
		PossibleCauses: []string{
			"Do kích ứng tại chỗ quanh mép môi.",
		},
		SoothingTips: []string{
			"Không nặn, không bóc chùm đang đỏ.",
			"Há miệng đau tăng thì nên khám da liễu.",
		},
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "chin", Concern: "irritation", Severity: "mild", Note: "Viêm cấp sát mép miệng — không chùm hạt đỏ sưng rõ."},
			{Region: "forehead", Concern: "not_visible", Severity: "mild", Note: "Không thấy trán trên ảnh — chụp đủ mặt mới nhận xét được."},
			{Region: "nose", Concern: "not_visible", Severity: "mild", Note: "Không thấy mũi trên ảnh — chụp đủ mặt mới nhận xét được."},
			{Region: "cheeks", Concern: "not_visible", Severity: "mild", Note: "Không thấy má trên ảnh — chụp đủ mặt mới nhận xét được."},
		},
	}
	q := "Cho e hỏi thâm 2 mép môi và dưới cằm của e phải làm sao ạ"
	if !AlignAdminSkinAnalysisWithQuestion(a, q, "vi") {
		t.Fatal("expected peri-oral align to rewrite wrong viêm cấp")
	}
	blob := strings.ToLower(a.Overview + " " + a.AttentionAreas[0].Note)
	if strings.Contains(blob, "viêm cấp") {
		t.Fatalf("viêm cấp should be rewritten to thâm, got overview=%q note=%q", a.Overview, a.AttentionAreas[0].Note)
	}
	if a.AttentionAreas[0].Concern != "pigmentation" {
		t.Fatalf("concern want pigmentation, got %q", a.AttentionAreas[0].Concern)
	}
	for _, tip := range a.SoothingTips {
		tl := strings.ToLower(tip)
		if strings.Contains(tl, "không nặn") || strings.Contains(tl, "không bóc") {
			t.Fatalf("acute tip should be removed, got %q", tip)
		}
	}
	joinedTips := strings.ToLower(strings.Join(a.SoothingTips, " "))
	if !strings.Contains(joinedTips, "chống nắng") && !strings.Contains(joinedTips, "thâm") {
		t.Fatalf("expected pigment tips, got %#v", a.SoothingTips)
	}
}

func TestAlignPeriOralThamVsAcute_StrongRedClusterKeepsB(t *testing.T) {
	t.Parallel()
	a := &dto.AdminSkinReviewAnalysis{
		Overview: "Crop sát khóe miệng. Chùm hạt đỏ sưng ngay viền môi — viêm cấp sát mép.",
		PossibleCauses: []string{
			"Do kích ứng tại chỗ quanh mép môi.",
		},
		SoothingTips: []string{
			"Không nặn, không bóc chùm đang đỏ.",
		},
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "chin", Concern: "irritation", Severity: "moderate", Note: "Chùm hạt đỏ sưng sát mép — viêm cấp sát mép miệng."},
		},
	}
	q := "Cho e hỏi thâm 2 mép môi phải làm sao ạ" // Q says thâm, but photo is clear acute cluster
	beforeOverview := a.Overview
	beforeConcern := a.AttentionAreas[0].Concern
	_ = AlignAdminSkinAnalysisWithQuestion(a, q, "vi")
	if a.Overview != beforeOverview {
		t.Fatalf("strong red cluster must keep B overview; before=%q after=%q", beforeOverview, a.Overview)
	}
	if a.AttentionAreas[0].Concern != beforeConcern {
		t.Fatalf("strong red cluster must keep irritation concern, got %q", a.AttentionAreas[0].Concern)
	}
	if !strings.Contains(strings.ToLower(a.Overview), "viêm cấp") {
		t.Fatalf("expected keep viêm cấp, got %q", a.Overview)
	}
}

func TestCauseLooksCircularPigment(t *testing.T) {
	t.Parallel()
	if !causeLooksCircularPigment("Do thâm trên má.") {
		t.Fatal("expected circular do thâm")
	}
	if !causeLooksCircularPigment("Do sắc tố.") {
		t.Fatal("expected circular sắc tố")
	}
	if causeLooksCircularPigment("Do thâm sau mụn hoặc nắng cục bộ.") {
		t.Fatal("post-acne/sun should stay")
	}
}

func TestAlignPigmentPrimaryTips(t *testing.T) {
	t.Parallel()
	a := &dto.AdminSkinReviewAnalysis{
		Overview: "Má đang thâm nâu nông.",
		PhotoNotes: "Close-up má — thiếu trán.",
		PossibleCauses: []string{"Do thâm.", "Do nắng cục bộ."},
		SoothingTips: []string{
			"Đừng nặn ổ đang sưng đỏ.",
			"Rửa dịu, tạm nghỉ sản phẩm mạnh đang dùng.",
		},
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "cheeks", Concern: "pigmentation", Severity: "mild", Note: "Má thâm nâu nông."},
			{Region: "forehead", Concern: "not_visible", Severity: "mild", Note: "Không thấy trán trên ảnh — chụp đủ mặt mới nhận xét được."},
			{Region: "nose", Concern: "not_visible", Severity: "mild", Note: "Không thấy mũi trên ảnh — chụp đủ mặt mới nhận xét được."},
			{Region: "chin", Concern: "not_visible", Severity: "mild", Note: "Không thấy cằm trên ảnh — chụp đủ mặt mới nhận xét được."},
		},
	}
	q := "Cho em hỏi laser trị thâm ở HCM chỗ nào tốt ạ, bao nhiêu buổi?"
	if !AlignAdminSkinAnalysisWithQuestion(a, q, "vi") {
		t.Fatal("expected pigment+laser alignment")
	}
	for _, c := range a.PossibleCauses {
		if causeLooksCircularPigment(c) {
			t.Fatalf("circular cause left: %q", c)
		}
	}
	for _, tip := range a.SoothingTips {
		if tipLooksInflamedOnly(tip) || tipLooksPauseStrongProduct(tip) {
			t.Fatalf("inflamed/pause tip left on pigment case: %q", tip)
		}
	}
	joined := strings.ToLower(strings.Join(a.SoothingTips, " "))
	if !strings.Contains(joined, "chống nắng") {
		t.Fatalf("expected SPF tip, got %#v", a.SoothingTips)
	}
	if !strings.Contains(joined, "laser") && !strings.Contains(joined, "bác sĩ") {
		t.Fatalf("expected local-derm/laser tip, got %#v", a.SoothingTips)
	}
}

func TestAlignPigmentEmptyFallbackNotInflamed(t *testing.T) {
	t.Parallel()
	a := &dto.AdminSkinReviewAnalysis{
		Overview:       "Má đang thâm nâu nông.",
		PhotoNotes:     "Close-up má.",
		PossibleCauses: []string{"Do thâm."},
		SoothingTips:   []string{"Đừng nặn ổ đang sưng đỏ."},
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "cheeks", Concern: "pigmentation", Severity: "mild", Note: "Má thâm nâu."},
			{Region: "forehead", Concern: "not_visible", Severity: "mild", Note: "Không thấy trán trên ảnh — chụp đủ mặt mới nhận xét được."},
			{Region: "nose", Concern: "not_visible", Severity: "mild", Note: "Không thấy mũi trên ảnh — chụp đủ mặt mới nhận xét được."},
			{Region: "chin", Concern: "not_visible", Severity: "mild", Note: "Không thấy cằm trên ảnh — chụp đủ mặt mới nhận xét được."},
		},
	}
	AlignAdminSkinAnalysisWithQuestion(a, "", "vi")
	for _, tip := range a.SoothingTips {
		if tipLooksInflamedOnly(tip) {
			t.Fatalf("pigment fallback must not re-inject inflamed tip: %q", tip)
		}
	}
	for _, c := range a.PossibleCauses {
		if causeLooksCircularPigment(c) {
			t.Fatalf("pigment fallback must not be circular: %q", c)
		}
		if strings.Contains(strings.ToLower(c), "dầu bít") {
			t.Fatalf("pigment fallback must not default to oil clog: %q", c)
		}
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

func TestQuestionAsksLaserNarrow(t *testing.T) {
	t.Parallel()
	if !questionAsksLaser("laser trị thâm ở HCM") {
		t.Fatal("laser should match")
	}
	if !questionAsksLaser("muốn trị thâm má") {
		t.Fatal("trị thâm should match")
	}
	if questionAsksLaser("em muốn đi bệnh viện da liễu kiểm tra") {
		t.Fatal("bare bệnh viện must not match")
	}
	if questionAsksLaser("phòng khám gần nhà có tốt không") {
		t.Fatal("bare phòng khám must not match")
	}
}

func TestQuestionImpliesSpotProductAHAToken(t *testing.T) {
	t.Parallel()
	if !questionImpliesSpotProduct("em đang dùng BHA 2%") {
		t.Fatal("BHA token should match")
	}
	if !questionImpliesSpotProduct("mới bôi AHA") {
		t.Fatal("AHA token should match")
	}
	if questionImpliesSpotProduct("da em khá ổn hôm nay") {
		t.Fatal("no active mention — must not match via substring")
	}
}

func TestVisionSafeUserQuestionHintShopping(t *testing.T) {
	t.Parallel()
	q := "Cho em hỏi laser trị thâm má ở HCM chỗ nào tốt ạ? Bao nhiêu buổi với giá sao ạ?"
	safe := VisionSafeUserQuestionHint(q)
	if safe == q {
		t.Fatal("shopping laser Q must be rewritten for vision")
	}
	low := strings.ToLower(safe)
	if strings.Contains(low, "chỗ nào tốt") || strings.Contains(low, "giá sao") || strings.Contains(low, "bao nhiêu buổi") {
		t.Fatalf("safe hint still has shopping language: %q", safe)
	}
	if !strings.Contains(low, "thâm") {
		t.Fatalf("safe hint should keep thâm intent: %q", safe)
	}
	plain := VisionSafeUserQuestionHint("em da nhiều dầu và đang bôi chấm mụn")
	if plain != "em da nhiều dầu và đang bôi chấm mụn" {
		t.Fatalf("non-shopping Q should pass through, got %q", plain)
	}
}

func TestTrimOverviewCheekOverlap(t *testing.T) {
	t.Parallel()
	a := &dto.AdminSkinReviewAnalysis{
		Overview: "Má đang thâm nâu nông. Vùng cằm nhìn ổn.",
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "cheeks", Concern: "pigmentation", Severity: "mild", Note: "Má thâm nâu nông rõ hơn gần tai."},
		},
	}
	if !trimOverviewCheekOverlap(a) {
		t.Fatal("expected overview↔cheek thâm overlap trim")
	}
	low := strings.ToLower(a.Overview)
	if strings.Contains(low, "thâm") {
		t.Fatalf("thâm detail should leave overview when cheek note already has it: %q", a.Overview)
	}
	if !strings.Contains(low, "cằm") {
		t.Fatalf("non-overlap sentence should remain: %q", a.Overview)
	}
}
