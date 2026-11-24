package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

func TestQA_ReviewSentencesThroughPolish(t *testing.T) {
	note := "Nhớ ghi chú lại cảm giác da tối nay nha."
	if got := stripInventedUserNote(note); got != note {
		t.Fatalf("advice dropped: %q", got)
	}
	out := &CoachStructuredOutput{SituationAnalysis: note}
	sanitizeWebCoachOutput(out, webQA{NoteKnown: true, HasUserNote: false})
	if out.SituationAnalysis != note {
		t.Fatalf("polish dropped advice: %q", out.SituationAnalysis)
	}

	cmp := &CoachStructuredOutput{SituationAnalysis: "Da ổn. So với lần trước, vùng này trông sưng hơn."}
	sanitizeWebCoachOutput(cmp, webQA{GuardComparison: true, PreviousComparable: false})
	if cmp.SituationAnalysis != "Da ổn." {
		t.Fatalf("comparison survived %q", cmp.SituationAnalysis)
	}
	trunc := &CoachStructuredOutput{SituationAnalysis: "Da ổn. Má trái kém lần trước."}
	sanitizeWebCoachOutput(trunc, webQA{GuardComparison: true, PreviousComparable: false, SwapCheeks: true})
	if trunc.SituationAnalysis != "Da ổn." || strings.Contains(trunc.SituationAnalysis, "má phải") {
		t.Fatalf("fragment %q", trunc.SituationAnalysis)
	}
	const advice = "Lần sau chụp giống lần trước để dễ so hơn nha."
	keep := &CoachStructuredOutput{SituationAnalysis: advice}
	sanitizeWebCoachOutput(keep, webQA{GuardComparison: true, PreviousComparable: false})
	if keep.SituationAnalysis != advice {
		t.Fatalf("%q -> %q", advice, keep.SituationAnalysis)
	}
	if got := stripInvalidComparison("Da đỡ hơn lần trước."); got != "" {
		t.Fatalf("đỡ hơn lần trước stayed %q", got)
	}
	for _, keep := range []string{
		"Lần trước mày bảo hay rát, hôm nay đỡ chưa?",
		"Nếu thấy đỡ rát thì cứ giữ routine như lần trước nha.",
		"Lần trước mày dùng kem gì mà đỡ khô vậy?",
		"Lần sau chụp giống lần trước để dễ so hơn nha.",
		"Lần sau rửa mặt nhẹ hơn lần trước nha.",
		"Mày vẫn rửa mặt như hôm qua là được.",
		"Thoa kem dày hơn hôm qua nha.",
		"Hôm qua mày ngủ muộn thì tối nay ngủ sớm hơn nha.",
	} {
		if got := stripInvalidComparison(keep); got != keep {
			t.Fatalf("%q -> %q", keep, got)
		}
		out := &CoachStructuredOutput{SituationAnalysis: keep}
		sanitizeWebCoachOutput(out, webQA{GuardComparison: true, PreviousComparable: false})
		if out.SituationAnalysis != keep {
			t.Fatalf("polish %q -> %q", keep, out.SituationAnalysis)
		}
	}
	for _, drop := range []string{
		"Da đỡ hơn lần trước, không có gì lạ.",
		"Hôm nay đỏ hơn hôm qua.",
		"So với tuần trước, da khô hơn.",
		"Da đỡ đỏ hơn hôm trước rồi.",
		"Lần trước da đỏ hơn.",
		"Lần trước nốt to hơn nhiều.",
		"Má trái vẫn đỏ như lần trước.",
		"So với lần trước, vùng này trông sưng hơn.",
		"Má trái kém lần trước.",
		"Da đỡ hơn lần trước.",
		"Má trái đỡ sưng rồi, lần trước to hơn.",
		"Da đỏ hơn lần trước nha.",
		"So với lần trước da đỡ hơn nha.",
		"Da mày kém lần trước nha.",
		"Da đỡ hơn lần trước là được rồi.",
		"Má trái đỡ sưng hơn hôm qua rồi đó nha.",
	} {
		if got := stripInvalidComparison(drop); got != "" {
			t.Fatalf("%q -> %q", drop, got)
		}
	}
	const echoed = "Hôm nay đỏ hơn hôm qua."
	if got := stripInvalidComparison(echoed); got != "" {
		t.Fatalf("without a user note %q -> %q", echoed, got)
	}
	echo := &CoachStructuredOutput{SituationAnalysis: echoed}
	sanitizeWebCoachOutput(echo, webQA{GuardComparison: true, PreviousComparable: false, UserSaidHomQua: true})
	if echo.SituationAnalysis != echoed {
		t.Fatalf("user said hôm qua: %q -> %q", echoed, echo.SituationAnalysis)
	}
	anchored := &CoachStructuredOutput{SituationAnalysis: "Hôm qua đỏ hơn lần trước."}
	sanitizeWebCoachOutput(anchored, webQA{GuardComparison: true, PreviousComparable: false, UserSaidHomQua: true})
	if strings.TrimSpace(anchored.SituationAnalysis) == "Hôm qua đỏ hơn lần trước." {
		t.Fatalf("other time anchor kept: %q", anchored.SituationAnalysis)
	}
	still := &CoachStructuredOutput{SituationAnalysis: "Da ổn. Má trái kém lần trước."}
	sanitizeWebCoachOutput(still, webQA{GuardComparison: true, PreviousComparable: false, UserSaidHomQua: true})
	if still.SituationAnalysis != "Da ổn." {
		t.Fatalf("lần trước still compared: %q", still.SituationAnalysis)
	}
}

func TestQA_InventedUserNote(t *testing.T) {
	const in = "Mày ghi chú là mụn ẩn"
	if got := stripInventedUserNote(in); strings.Contains(got, "ghi chú") || strings.Contains(got, "mày ghi") {
		t.Fatalf("invented note stayed %q", got)
	}
	kept := stripInventedUserNote("Da ổn. Mày ghi chú là mụn ẩn.")
	if strings.Contains(kept, "ghi chú") || !strings.Contains(kept, "Da ổn") {
		t.Fatalf("mixed note %q", kept)
	}
	if got := stripInventedUserNote("Mày ghi da hôm nay ổn."); got != "Mày ghi da hôm nay ổn." {
		t.Fatalf("ordinary mày ghi changed %q", got)
	}
	const claimed = "Mày có ghi là…"
	if got := stripInventedUserNote(claimed); got != "" {
		t.Fatalf("%q -> %q", claimed, got)
	}
	mixed := &CoachStructuredOutput{SituationAnalysis: "Da ổn. Mày có ghi là…"}
	sanitizeWebCoachOutput(mixed, webQA{NoteKnown: true, HasUserNote: false})
	if mixed.SituationAnalysis != "Da ổn." {
		t.Fatalf("Mày có ghi là… stayed in %q", mixed.SituationAnalysis)
	}
}

func TestQA_CameraFrontAndRear(t *testing.T) {
	// Unmirrored front and rear both show the user's left cheek on the image's right.
	if MapFrameCheek("left", false) != "right" || MapFrameCheek("right", false) != "left" {
		t.Fatal("unmirrored photo should swap")
	}
	if MapFrameCheek("left", true) != "left" || MapFrameCheek("right", true) != "right" {
		t.Fatal("mirrored file should keep the image edge")
	}
	for _, phrase := range []string{"má bên trái", "phía trái cằm", "bên trái trán"} {
		got := FlipFrameCheeks(phrase)
		if strings.Contains(got, "trái") || !strings.Contains(got, "phải") {
			t.Fatalf("%q -> %q", phrase, got)
		}
	}
	side := &CoachStructuredOutput{SituationAnalysis: "phía phải cằm"}
	sanitizeWebCoachOutput(side, webQA{})
	if side.SituationAnalysis != "bên phải cằm" {
		t.Fatalf("phía phải cằm -> %q", side.SituationAnalysis)
	}
	left := &CoachStructuredOutput{SituationAnalysis: "phía trái cằm"}
	sanitizeWebCoachOutput(left, webQA{})
	if left.SituationAnalysis != "bên trái cằm" {
		t.Fatalf("phía trái cằm -> %q", left.SituationAnalysis)
	}
	lean := &CoachStructuredOutput{SituationAnalysis: "nằm nghiêng bên phải"}
	sanitizeWebCoachOutput(lean, webQA{SwapCheeks: true})
	if lean.SituationAnalysis != "nằm nghiêng bên phải" {
		t.Fatalf("nằm nghiêng bên phải -> %q", lean.SituationAnalysis)
	}
	for _, tc := range []struct{ in, want string }{
		{"cằm trái", "cằm phải"},
		{"cằm phải", "cằm trái"},
		{"trán phải", "trán trái"},
		{"trán trái", "trán phải"},
	} {
		if got := FlipFrameCheeks(tc.in); got != tc.want {
			t.Fatalf("%q -> %q, want %q", tc.in, got, tc.want)
		}
	}
	raw := `[{"zone":"left_cheek","note":"má trái đỏ"}]`
	out := &CoachStructuredOutput{ZoneNotesRaw: []byte(raw)}
	sanitizeWebCoachOutput(out, webQA{SwapCheeks: true, Camera: "rear"})
	text := string(out.ZoneNotesRaw)
	if !strings.Contains(text, `"zone":"right_cheek"`) || !strings.Contains(text, "má phải") || strings.Contains(text, "left_cheek") || strings.Contains(text, "má trái") {
		t.Fatalf("zone key and note diverged: %s", text)
	}
	closeup := &CoachStructuredOutput{ZoneNotesRaw: []byte(raw)}
	sanitizeWebCoachOutput(closeup, webQA{SwapCheeks: false, KeepZone: "left_cheek"})
	kept := string(closeup.ZoneNotesRaw)
	if !strings.Contains(kept, `"zone":"left_cheek"`) || !strings.Contains(kept, "má trái") {
		t.Fatalf("user-picked close-up was swapped: %s", kept)
	}
}

func TestQA_CompareOnlyWhenPreviousIsClearAndSameArea(t *testing.T) {
	const in = "Da đỡ hơn lần trước."
	if got := stripInvalidComparison(in); strings.Contains(got, "lần trước") {
		t.Fatalf("invalid comparison stayed %q", got)
	}
	score := 0.8
	facts := CheckInCoachFacts{History: []CheckInHistoryFact{{
		PhotoEvidence: PhotoEvidenceOK,
		Zones:         []string{"left_cheek"},
		Score:         &score,
	}}}
	if !previousComparable(facts, []string{"left_cheek"}) {
		t.Fatal("clear same-area previous should compare")
	}
	blurry := facts
	blurry.History[0].PhotoEvidence = PhotoEvidenceLimited
	if previousComparable(blurry, []string{"left_cheek"}) {
		t.Fatal("blurry previous must not compare")
	}
	if previousComparable(facts, []string{"chin"}) {
		t.Fatal("different area must not compare")
	}
	if got := stripInvalidComparison("Da đỡ hơn lần trước."); strings.Contains(got, "hơn lần trước") {
		t.Fatal(got)
	}
}

func TestQA_SmoothSkinDoesNotClaimBumps(t *testing.T) {
	for _, in := range []string{
		"Da mịn, chưa thấy mụn ẩn",
		"không thấy mụn ẩn rõ",
		"không phải mụn ẩn",
	} {
		if got := softenSmoothSkinClaim(in); got != in {
			t.Fatalf("%q became %q", in, got)
		}
	}
	for _, in := range []string{
		"Da mịn màng, có milia ở hai má.",
		"Da mịn màng, mụn ẩn hoặc milia.",
	} {
		got := softenSmoothSkinClaim(in)
		if strings.Contains(strings.ToLower(got), "milia") || strings.Contains(got, "có có") || strings.Contains(got, "có vài nốt nhỏ") {
			t.Fatalf("%q became %q", in, got)
		}
	}
	if got := softenSmoothSkinClaim("không thấy milia"); got != "không thấy milia" {
		t.Fatalf("negated milia %q", got)
	}
	const spots = "Da mịn màng nhưng cằm có 3 nốt mụn ẩn sần rõ."
	spotsOut := &CoachStructuredOutput{SituationAnalysis: spots}
	sanitizeWebCoachOutput(spotsOut, webQA{})
	if spotsOut.SituationAnalysis != spots {
		t.Fatalf("%q -> %q", spots, spotsOut.SituationAnalysis)
	}
	oil := &CoachStructuredOutput{SituationAnalysis: "Dầu làm da trông không sạch."}
	sanitizeWebCoachOutput(oil, webQA{})
	if oil.SituationAnalysis != "Da dễ bóng dầu." {
		t.Fatalf("Dầu làm da trông không sạch. -> %q, want %q", oil.SituationAnalysis, "Da dễ bóng dầu.")
	}
	other := &CoachStructuredOutput{SituationAnalysis: "Da dầu làm da trông không sạch."}
	sanitizeWebCoachOutput(other, webQA{})
	if other.SituationAnalysis != "Da dễ bóng dầu." {
		t.Fatalf("oil sentence repeated dầu: %q", other.SituationAnalysis)
	}
	const oily = "Lớp dầu nhờn làm da trông không sạch lắm, nhưng không sao!"
	keep := &CoachStructuredOutput{SituationAnalysis: oily}
	sanitizeWebCoachOutput(keep, webQA{})
	if keep.SituationAnalysis != "Da dễ bóng dầu, nhưng không sao." {
		t.Fatalf("%q -> %q", oily, keep.SituationAnalysis)
	}
	const milia = "Da mịn màng, có hạt milia nhỏ li ti ở hai má."
	if got := softenSmoothSkinClaim(milia); strings.Contains(strings.ToLower(got), "milia") || strings.Contains(got, "hạt") {
		t.Fatalf("%q -> %q", milia, got)
	}
	miliaOut := &CoachStructuredOutput{SituationAnalysis: milia}
	sanitizeWebCoachOutput(miliaOut, webQA{})
	if miliaOut.SituationAnalysis != "Tao xem ảnh hôm nay rồi nha mày." {
		t.Fatalf("%q -> %q", milia, miliaOut.SituationAnalysis)
	}
	const compared = "Má trái đỡ sưng rồi, lần trước to hơn."
	if got := stripInvalidComparison(compared); got != "" {
		t.Fatalf("%q -> %q", compared, got)
	}
	secondary := &CoachStructuredOutput{SummaryNotes: compared}
	sanitizeWebCoachOutput(secondary, webQA{GuardComparison: true, PreviousComparable: false})
	if secondary.SummaryNotes != "" {
		t.Fatalf("secondary comparison stayed %q", secondary.SummaryNotes)
	}
}

func TestQA_MainFieldDoesNotGoEmpty(t *testing.T) {
	smooth := &CoachStructuredOutput{SituationAnalysis: "Da mịn màng, đây là mụn ẩn."}
	sanitizeWebCoachOutput(smooth, webQA{})
	if smooth.SituationAnalysis != "Tao xem ảnh hôm nay rồi nha mày." {
		t.Fatalf("smooth-only -> %q", smooth.SituationAnalysis)
	}
	cmp := &CoachStructuredOutput{SituationAnalysis: "So với lần trước, vùng này trông sưng hơn."}
	sanitizeWebCoachOutput(cmp, webQA{GuardComparison: true, PreviousComparable: false})
	if cmp.SituationAnalysis != "Tao xem ảnh hôm nay rồi nha mày." {
		t.Fatalf("comparison-only -> %q", cmp.SituationAnalysis)
	}
	kept := &CoachStructuredOutput{SituationAnalysis: "Da ổn. So với lần trước da đỏ hơn."}
	sanitizeWebCoachOutput(kept, webQA{GuardComparison: true, PreviousComparable: false})
	if kept.SituationAnalysis != "Da ổn." {
		t.Fatalf("mixed -> %q", kept.SituationAnalysis)
	}
}

func TestQA_EmptyImprovementsDropped(t *testing.T) {
	qa := webQA{GuardComparison: true, PreviousComparable: false}
	mixed := &CoachStructuredOutput{Improvements: []struct {
		Tip string `json:"tip"`
		Why string `json:"why"`
	}{
		{Tip: "Má trái đỡ sưng rồi, lần trước to hơn.", Why: "So với lần trước da đỏ hơn."},
		{Tip: "Rửa mặt dịu.", Why: "Da đang đỏ."},
	}}
	sanitizeWebCoachOutput(mixed, qa)
	if len(mixed.Improvements) != 1 || mixed.Improvements[0].Tip != "Rửa mặt dịu." || mixed.Improvements[0].Why != "Da đang đỏ." {
		t.Fatalf("mixed %#v", mixed.Improvements)
	}
	all := &CoachStructuredOutput{Improvements: []struct {
		Tip string `json:"tip"`
		Why string `json:"why"`
	}{
		{Tip: "Má trái đỡ sưng rồi, lần trước to hơn.", Why: "So với lần trước da đỏ hơn."},
		{Tip: "So với lần trước, vùng này trông sưng hơn.", Why: "Da đỡ hơn lần trước."},
	}}
	sanitizeWebCoachOutput(all, qa)
	if all.Improvements == nil {
		t.Fatal("all-empty improvements became null")
	}
	if len(all.Improvements) != 0 {
		t.Fatalf("all-empty %#v", all.Improvements)
	}
	raw, err := json.Marshal(all.Improvements)
	if err != nil || string(raw) != "[]" {
		t.Fatalf("json %s err %v", raw, err)
	}
}

func TestQA_ReassuranceOnceAndNotOnBlur(t *testing.T) {
	line := "Không sao đâu, da có hôm lên hôm xuống. Da đỏ. Không sao đâu, da có hôm lên hôm xuống."
	fields := []polishField{{text: &line, opening: true}}
	dedupeReassurance(fields, true)
	if strings.Count(strings.ToLower(*fields[0].text), "da có hôm lên hôm xuống") != 1 {
		t.Fatalf("dedupe %q", *fields[0].text)
	}
	blurry := "Không sao đâu, da có hôm lên hôm xuống. Chụp lại cho rõ."
	bfields := []polishField{{text: &blurry, opening: true}}
	dedupeReassurance(bfields, false)
	if strings.Contains(strings.ToLower(*bfields[0].text), "hôm lên hôm xuống") {
		t.Fatalf("blurry kept reassurance %q", *bfields[0].text)
	}
	if !strings.Contains(*bfields[0].text, "Chụp lại") {
		t.Fatalf("retake lost %q", *bfields[0].text)
	}
}

func TestQA_MayCountRewritesNaturally(t *testing.T) {
	for _, in := range []string{"Có một nốt mới ở má.", "Da hôm nay ổn.", "So với da hôm nay ổn."} {
		out := &CoachStructuredOutput{SituationAnalysis: in}
		sanitizeWebCoachOutput(out, webQA{})
		if strings.HasPrefix(out.SituationAnalysis, "Tao thấy") {
			t.Fatalf("prefix injected into %q", out.SituationAnalysis)
		}
		if out.SituationAnalysis != in {
			t.Fatalf("%q became %q", in, out.SituationAnalysis)
		}
	}
	if strings.Contains(plainLoanwords("Mình đang stressed."), "stressed") {
		t.Fatal("stressed was not translated")
	}
	if !strings.Contains(plainLoanwords("Mình đang stressed."), "căng thẳng") {
		t.Fatal("stressed missing plain Vietnamese")
	}
}

func TestQA_TreatBecomesDacTri(t *testing.T) {
	const in = "(và sau treat nếu có)"
	got := plainLoanwords(in)
	if got != "(và sau đó là đặc trị nếu có)" {
		t.Fatalf("%q -> %q", in, got)
	}
	full := plainLoanwords("Lớp mỏng sau rửa mặt (và sau treat nếu có).")
	if full != "Lớp mỏng sau rửa mặt (và sau đó là đặc trị nếu có)." {
		t.Fatalf("sentence %q", full)
	}
	const titled = "Treat mụn buổi tối thôi nha."
	if got := plainLoanwords(titled); got != "Đặc trị mụn buổi tối thôi nha." {
		t.Fatalf("%q -> %q", titled, got)
	}
	if got := plainLoanwords("để check da"); got != "để kiểm tra da" {
		t.Fatalf("check %q", got)
	}
	if plainLoanwords("check-up") != "check-up" || plainLoanwords("check-list") != "check-list" {
		t.Fatal("hyphenated check was rewritten")
	}
	if plainLoanwords("…/check?x=1") != "…/check?x=1" {
		t.Fatal("link check was rewritten")
	}
	if got := plainLoanwords("Check da"); got != "Kiểm tra da" {
		t.Fatalf("Check %q", got)
	}
	if plainLoanwords("check-in") != "check-in" || plainLoanwords("Check-in") != "Check-in" {
		t.Fatal("check-in was rewritten")
	}
	if got := plainLoanwords("Đừng nặn, đừng tự lẻ."); got != "Đừng nặn, đừng tự chích." || strings.Contains(got, "tự nặn") {
		t.Fatalf("tự lẻ %q", got)
	}
	for _, keep := range []string{"Routine", "Toner", "check-in", "treatment"} {
		if plainLoanwords(keep) != keep {
			t.Fatalf("%q changed", keep)
		}
	}
}

func TestQA_LoanwordsKeepBrands(t *testing.T) {
	got := plainLoanwords("Dùng toner và serum, đừng makeup khi stress, tắt flash, bỏ filter.")
	for _, want := range []string{"nước cân bằng da", "tinh chất", "trang điểm", "căng thẳng", "đèn flash", "bộ lọc màu"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	for _, gone := range []string{"toner", "serum", "makeup", "stress", "filter"} {
		if strings.Contains(strings.ToLower(got), gone) {
			t.Fatalf("%q still in %q", gone, got)
		}
	}
	brand := plainLoanwords("Klairs Redness Solutions")
	if brand != "Klairs Redness Solutions" {
		t.Fatalf("brand changed %q", brand)
	}
	if !strings.Contains(plainLoanwords("BHA (tẩy da chết trong lỗ chân lông)"), "BHA") {
		t.Fatal("BHA name dropped")
	}
}

func TestQA_HedgeMovesToEnd(t *testing.T) {
	const in = "có vài nốt nhỏ, chưa chắc ở trán"
	got := moveHedgeToClauseEnd(in)
	if got != "có vài nốt nhỏ ở trán, chưa chắc" {
		t.Fatalf("got %q", got)
	}
}

func TestWebVI_ProdPhrases(t *testing.T) {
	const (
		acneSit      = "Hôm nay vùng má trái đang có khá nhiều mụn viêm nổi rõ, da ửng đỏ ở chỗ đó. Bề mặt da không đều, nhìn có vẻ da đang bị kích ứng."
		acneBar      = "Vùng má trái có khá nhiều mụn viêm, chưa rõ ràng."
		acneSafe     = "Thay đổi một sản phẩm mới tại một thời điểm — patch-test (thử trước ở một vùng nhỏ) trước ở vùng nhỏ nếu dùng cái gì lạ."
		clearSit     = "Hôm nay hai bên má của mày có khá nhiều nốt nhỏ màu da nổi cao, trông giống mụn ẩn hoặc milia (hạt nhỏ trắng dưới da)."
		clearConcern = "Mày ghi là quan tâm mụn ẩn, và ảnh hôm nay thật sự có khá nhiều nốt nhỏ màu da nổi cao ở hai bên má — đó là dấu hiệu của mụn ẩn hoặc milia. Da không đỏ rõ, nên tao sẽ khuyên dịu trước, rồi sau đó mới xem xét thêm chất tẩy da chết nhẹ."
		clearBar     = "Hai bên má có khá nhiều nốt nhỏ màu da nổi cao — đó là dấu hiệu mụn ẩn hoặc milia, không phải mụn đỏ sưng."
		zoneNote     = "Nhiều nốt nhỏ màu da nổi cao, trông giống mụn ẩn hoặc milia."
	)
	qa := webQA{Locale: "vi", NoteKnown: true, HasUserNote: false}
	acne := &CoachStructuredOutput{
		SituationAnalysis: acneSit,
		SafetyReminders:   []string{acneSafe},
		SkinScoreNotesRaw: []byte(`{"clarity":"` + acneBar + `"}`),
		// Not shown on the web screen. Four «mày» here used to block the opening.
		SummaryNotes: "Mày chụp đủ sáng. Mày giữ góc ổn.",
		Strengths:    []string{"Mày không nặn.", "Mày kiên nhẫn."},
	}
	sanitizeWebCoachOutput(acne, qa)
	if acne.SituationAnalysis != "Hôm nay vùng má trái của mày đang có khá nhiều mụn viêm nổi rõ, da ửng đỏ ở chỗ đó. Bề mặt da không đều, nhìn có vẻ da đang bị kích ứng." {
		t.Fatalf("acne situation %q", acne.SituationAnalysis)
	}
	if len(acne.SafetyReminders) != 1 || acne.SafetyReminders[0] != "Thay đổi một sản phẩm mới tại một thời điểm — thử trước ở một vùng nhỏ nếu dùng cái gì lạ." {
		t.Fatalf("acne safety %q", acne.SafetyReminders)
	}
	var scores map[string]string
	if err := json.Unmarshal(acne.SkinScoreNotesRaw, &scores); err != nil {
		t.Fatal(err)
	}
	if scores["clarity"] != "Vùng má trái có khá nhiều mụn viêm." {
		t.Fatalf("acne bar %q", scores["clarity"])
	}
	if acne.SummaryNotes != "Mày chụp đủ sáng. Mày giữ góc ổn." || len(acne.Strengths) != 2 {
		t.Fatalf("hidden fields changed %q %#v", acne.SummaryNotes, acne.Strengths)
	}

	clear := &CoachStructuredOutput{
		SituationAnalysis: clearSit,
		ConcernAlignment:  clearConcern,
		SkinScoreNotesRaw: []byte(`{"clarity":"` + clearBar + `"}`),
		ZoneNotesRaw: []byte(`[{"zone":"right_cheek","note":"` + zoneNote + `","severity":"mild"},` +
			`{"zone":"left_cheek","note":"` + zoneNote + `","severity":"mild"}]`),
	}
	sanitizeWebCoachOutput(clear, qa)
	if clear.SituationAnalysis != clearSit {
		t.Fatalf("clear situation changed %q", clear.SituationAnalysis)
	}
	const clearConcernOut = "Ảnh hôm nay thật sự có khá nhiều nốt nhỏ màu da nổi cao ở hai bên má — trông giống dấu hiệu mụn ẩn hoặc milia (hạt nhỏ trắng dưới da). Da không đỏ rõ, nên tao sẽ khuyên dịu trước, rồi sau đó mới xem xét thêm chất tẩy da chết nhẹ."
	if clear.ConcernAlignment != clearConcernOut {
		t.Fatalf("clear concern %q", clear.ConcernAlignment)
	}
	if err := json.Unmarshal(clear.SkinScoreNotesRaw, &scores); err != nil {
		t.Fatal(err)
	}
	if scores["clarity"] != "Hai bên má có khá nhiều nốt nhỏ màu da nổi cao — trông giống dấu hiệu mụn ẩn hoặc milia, không phải mụn đỏ sưng." {
		t.Fatalf("clear bar %q", scores["clarity"])
	}
	var zones []map[string]string
	if err := json.Unmarshal(clear.ZoneNotesRaw, &zones); err != nil {
		t.Fatal(err)
	}
	if len(zones) != 2 {
		t.Fatalf("zones %#v", zones)
	}
	if zones[0]["note"] != "Nhiều nốt nhỏ màu da nổi cao, trông giống mụn ẩn hoặc milia (hạt nhỏ trắng dưới da)." {
		t.Fatalf("first zone %q", zones[0]["note"])
	}
	if zones[1]["note"] != zoneNote {
		t.Fatalf("second zone %q", zones[1]["note"])
	}

	keptZone := "Hôm nay má trái của bạn đang đỏ."
	zoneOut := &CoachStructuredOutput{SituationAnalysis: keptZone}
	sanitizeWebCoachOutput(zoneOut, qa)
	if zoneOut.SituationAnalysis != keptZone {
		t.Fatalf("của stayed %q", zoneOut.SituationAnalysis)
	}
	already := &CoachStructuredOutput{SituationAnalysis: "Hôm nay da của mày đang ổn. Mai tính tiếp."}
	sanitizeWebCoachOutput(already, qa)
	if already.SituationAnalysis != "Hôm nay da của mày đang ổn. Mai tính tiếp." {
		t.Fatalf("already mày %q", already.SituationAnalysis)
	}
	da := &CoachStructuredOutput{SituationAnalysis: "Hôm nay da đang ổn. Mai tính tiếp."}
	sanitizeWebCoachOutput(da, qa)
	if da.SituationAnalysis != "Hôm nay da của mày đang ổn. Mai tính tiếp." {
		t.Fatalf("hôm nay da %q", da.SituationAnalysis)
	}
	two := &CoachStructuredOutput{
		SituationAnalysis: "Hôm nay da đang ổn.",
		SummaryNotes:      "Tao nhắn mày một câu. Rồi mày xem lại.",
	}
	sanitizeWebCoachOutput(two, qa)
	if two.SituationAnalysis != "Hôm nay da của mày đang ổn." {
		t.Fatalf("hidden mày blocked the opening %q", two.SituationAnalysis)
	}
	visible := &CoachStructuredOutput{
		SituationAnalysis: "Hôm nay da đang ổn.",
		SafetyReminders:   []string{"Nhớ mày. Rồi mày xem."},
	}
	sanitizeWebCoachOutput(visible, qa)
	if visible.SituationAnalysis != "Hôm nay da đang ổn." {
		t.Fatalf("visible mày still inserted %q", visible.SituationAnalysis)
	}
	face := &CoachStructuredOutput{SituationAnalysis: "Hôm nay da mặt hơi khô."}
	sanitizeWebCoachOutput(face, qa)
	if face.SituationAnalysis != "Hôm nay da mặt của mày hơi khô." {
		t.Fatalf("da mặt %q", face.SituationAnalysis)
	}
	if got := softenAssertedGroup("Đó là mụn ẩn hoặc milia."); got != "Trông giống mụn ẩn hoặc milia." {
		t.Fatalf("sentence start %q", got)
	}
	patch := &CoachStructuredOutput{SafetyReminders: []string{"Patch test trước nha."}}
	sanitizeWebCoachOutput(patch, qa)
	if patch.SafetyReminders[0] != "Thử trước ở một vùng nhỏ nha." {
		t.Fatalf("patch trước %q", patch.SafetyReminders[0])
	}
	beforeUse := &CoachStructuredOutput{SafetyReminders: []string{"Nhớ patch-test trước khi dùng."}}
	sanitizeWebCoachOutput(beforeUse, qa)
	if beforeUse.SafetyReminders[0] != "Nhớ thử ở một vùng nhỏ trước khi dùng." {
		t.Fatalf("trước khi %q", beforeUse.SafetyReminders[0])
	}
	neu := &CoachStructuredOutput{SituationAnalysis: "Nếu mày bảo là da rát thì dừng BHA nha."}
	sanitizeWebCoachOutput(neu, qa)
	if neu.SituationAnalysis != "Nếu mày bảo là da rát thì dừng BHA nha." {
		t.Fatalf("nếu mày %q", neu.SituationAnalysis)
	}
	theo := &CoachStructuredOutput{ConcernAlignment: "Theo mày nói là da ổn."}
	sanitizeWebCoachOutput(theo, qa)
	if strings.TrimSpace(theo.ConcernAlignment) != "" {
		t.Fatalf("theo mày stayed %q", theo.ConcernAlignment)
	}
	for _, keep := range []string{
		"Ảnh hơi tối nên mụn chưa rõ ràng.",
		"Chưa rõ ràng có mụn hay không.",
		"Ranh giới vùng đỏ chưa rõ ràng, chụp lại nha.",
		"Mày nhớ ghi chú lại cảm giác da mỗi tối nha.",
		"Bạn bè nói là kem này tốt.",
	} {
		got := viRewrite(keep, false, qa)
		if got != keep {
			t.Fatalf("kept %q -> %q", keep, got)
		}
	}
	en := &CoachStructuredOutput{SituationAnalysis: acneSit}
	sanitizeWebCoachOutput(en, webQA{Locale: "en"})
	if en.SituationAnalysis != acneSit {
		t.Fatalf("en changed %q", en.SituationAnalysis)
	}
	doctor := &CoachStructuredOutput{SituationAnalysis: "Bác sĩ nói là nên dịu thôi."}
	sanitizeWebCoachOutput(doctor, qa)
	if doctor.SituationAnalysis != "Bác sĩ nói là nên dịu thôi." {
		t.Fatalf("doctor %q", doctor.SituationAnalysis)
	}
	severe := &CoachStructuredOutput{ZoneNotesRaw: []byte(`[{"zone":"forehead","note":"Da hôm nay ổn, chưa rõ ràng.","severity":"moderate"}]`)}
	sanitizeWebCoachOutput(severe, qa)
	var sevZones []map[string]string
	if err := json.Unmarshal(severe.ZoneNotesRaw, &sevZones); err != nil {
		t.Fatal(err)
	}
	if len(sevZones) != 1 || sevZones[0]["note"] != "Da hôm nay ổn." {
		t.Fatalf("severe unclear %v", sevZones)
	}
	explained := &CoachStructuredOutput{ZoneNotesRaw: []byte(`[{"zone":"right_cheek","note":"Trông giống milia (hạt nhỏ trắng dưới da)."}]`)}
	sanitizeWebCoachOutput(explained, qa)
	var explainedZones []map[string]string
	if err := json.Unmarshal(explained.ZoneNotesRaw, &explainedZones); err != nil {
		t.Fatal(err)
	}
	if explainedZones[0]["note"] != "Trông giống milia (hạt nhỏ trắng dưới da)." {
		t.Fatalf("milia doubled %q", explainedZones[0]["note"])
	}
	android := &CoachStructuredOutput{SituationAnalysis: acneSit, SafetyReminders: []string{acneSafe}}
	applyCoachVoiceGuard(android, "android", qa)
	if android.SituationAnalysis != acneSit || android.SafetyReminders[0] != acneSafe {
		t.Fatalf("android changed %q %q", android.SituationAnalysis, android.SafetyReminders)
	}
	noted := &CoachStructuredOutput{ConcernAlignment: "Mày ghi là quan tâm mụn ẩn."}
	sanitizeWebCoachOutput(noted, webQA{Locale: "vi", NoteKnown: true, HasUserNote: true})
	if noted.ConcernAlignment != "Mày ghi là quan tâm mụn ẩn." {
		t.Fatalf("real note %q", noted.ConcernAlignment)
	}
	only := &CoachStructuredOutput{SituationAnalysis: "Mày ghi là quan tâm mụn ẩn."}
	sanitizeWebCoachOutput(only, qa)
	if only.SituationAnalysis != "Tao xem ảnh hôm nay rồi nha mày." {
		t.Fatalf("empty fallback %q", only.SituationAnalysis)
	}
}

func TestWebVI_MayCapScreens(t *testing.T) {
	const (
		clearSit = "Hôm nay da mày nhìn khá ổn — hai bên má mịn màng, không thấy mụn đỏ hay thâm nổi bật. Lớp ngoài da trông đều, không bóng dầu hay khô rát. Đây là nền tốt để mày xây dựng routine chăm sóc hàng ngày."
		clearCon = "Da mày hôm nay trông khá ổn — mịn màng, không viêm hay khô rát."
		zone     = "Trông giống da mịn màng, không có dấu hiệu nổi bật."
		acneSit  = "Hôm nay vùng má trái của mày có khá nhiều mụn viêm đỏ, bề mặt da cũng không đều."
		acneCon  = "Vùng má trái đang có mụn viêm đỏ rõ ràng và bề mặt da không đều. Tao khuyên tập trung vào làm dịu và dưỡng ẩm trước, để da phục hồi trước khi dùng bất kỳ chất trị mụn nào."
		acneWhy  = "Rau má làm dịu — ưu tiên khi da đang viêm đỏ sưng như vùng má mày hôm nay."
	)
	qa := webQA{Locale: "vi", NoteKnown: true, HasUserNote: false}
	clear := &CoachStructuredOutput{
		SituationAnalysis: clearSit,
		ConcernAlignment:  clearCon,
		SkinScoreNotesRaw: []byte(`{"overall":"` + clearCon + `"}`),
		ZoneNotesRaw: []byte(`[{"zone":"right_cheek","note":"` + zone + `","severity":"mild"},` +
			`{"zone":"left_cheek","note":"` + zone + `","severity":"mild"}]`),
		CareSuggestions: []CoachCareSuggestion{
			{SafetyNote: "Không cần rửa quá mạnh — da mày không cần điều đó."},
			{SafetyNote: "Chọn kem nhẹ nếu da mày dễ bóng dầu."},
		},
		RoutineHints: []string{"Mỗi ngày: Uống đủ nước, ngủ đủ giấc — da sẽ cảm ơn mày."},
		AvoidOrPatch: []string{"Tránh các sản phẩm có mùi thơm hoặc cồn cao nếu da mày nhạy cảm."},
		ProductGuidance: []dto.ProductGuidanceItem{{
			Why: "SPF50 hàng ngày là bước quan trọng nhất để bảo vệ da mày khỏi tia UV, giúp da luôn khỏe mạnh.",
		}},
	}
	sanitizeWebCoachOutput(clear, qa)
	if clear.SituationAnalysis != "Hôm nay da mày nhìn khá ổn — hai bên má mịn màng, không thấy mụn đỏ hay thâm nổi bật. Lớp ngoài da trông đều, không bóng dầu hay khô rát. Đây là nền tốt để mày xây dựng thói quen chăm da hàng ngày." {
		t.Fatalf("clear situation %q", clear.SituationAnalysis)
	}
	if clear.ConcernAlignment != "Da hôm nay trông khá ổn — mịn màng, không viêm hay khô rát." {
		t.Fatalf("clear concern %q", clear.ConcernAlignment)
	}
	var scores map[string]string
	if err := json.Unmarshal(clear.SkinScoreNotesRaw, &scores); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(scores["overall"]) != "" {
		t.Fatalf("overall still shown %q", scores["overall"])
	}
	var zones []map[string]string
	if err := json.Unmarshal(clear.ZoneNotesRaw, &zones); err != nil {
		t.Fatal(err)
	}
	for i, z := range zones {
		if z["note"] != "Da mịn màng, không có dấu hiệu nổi bật." {
			t.Fatalf("zone %d %q", i, z["note"])
		}
	}
	if clear.CareSuggestions[0].SafetyNote != "Không cần rửa quá mạnh — da không cần điều đó." {
		t.Fatalf("care0 %q", clear.CareSuggestions[0].SafetyNote)
	}
	if clear.CareSuggestions[1].SafetyNote != "Chọn kem nhẹ nếu da dễ bóng dầu." {
		t.Fatalf("care1 %q", clear.CareSuggestions[1].SafetyNote)
	}
	if clear.RoutineHints[0] != "Mỗi ngày: Uống đủ nước, ngủ đủ giấc — da sẽ khoẻ hơn." {
		t.Fatalf("routine %q", clear.RoutineHints[0])
	}
	if clear.AvoidOrPatch[0] != "Tránh các sản phẩm có mùi thơm hoặc cồn cao nếu da nhạy cảm." {
		t.Fatalf("avoid %q", clear.AvoidOrPatch[0])
	}
	if clear.ProductGuidance[0].Why != "SPF50 hàng ngày là bước quan trọng nhất để bảo vệ da khỏi tia UV, giúp da luôn khỏe mạnh." {
		t.Fatalf("product %q", clear.ProductGuidance[0].Why)
	}
	if n := screenMayCount(clear, decodePolishZones(clear.ZoneNotesRaw), decodePolishScores(clear.SkinScoreNotesRaw)); n > 2 {
		t.Fatalf("clear mày %d", n)
	}

	acne := &CoachStructuredOutput{
		SituationAnalysis: acneSit,
		ConcernAlignment:  acneCon,
		ProductGuidance:   []dto.ProductGuidanceItem{{Why: acneWhy}},
	}
	sanitizeWebCoachOutput(acne, qa)
	if acne.SituationAnalysis != acneSit {
		t.Fatalf("acne situation %q", acne.SituationAnalysis)
	}
	if acne.ConcernAlignment != "Vùng má trái đang có mụn viêm đỏ rõ ràng và bề mặt da không đều. Tao khuyên tập trung vào làm dịu và dưỡng ẩm, để da phục hồi trước khi dùng bất kỳ chất trị mụn nào." {
		t.Fatalf("acne concern %q", acne.ConcernAlignment)
	}
	if acne.ProductGuidance[0].Why != acneWhy {
		t.Fatalf("acne product %q", acne.ProductGuidance[0].Why)
	}
	if n := screenMayCount(acne, nil, nil); n != 2 {
		t.Fatalf("acne mày %d", n)
	}

	spot := &CoachStructuredOutput{ZoneNotesRaw: []byte(`[{"zone":"forehead","note":"Trông giống mụn viêm đỏ."}]`)}
	sanitizeWebCoachOutput(spot, qa)
	if err := json.Unmarshal(spot.ZoneNotesRaw, &zones); err != nil {
		t.Fatal(err)
	}
	if zones[0]["note"] != "Trông giống mụn viêm đỏ." {
		t.Fatalf("spot note %q", zones[0]["note"])
	}
	none := &CoachStructuredOutput{SituationAnalysis: "Bề mặt hôm nay đều."}
	sanitizeWebCoachOutput(none, qa)
	if none.SituationAnalysis != "Bề mặt hôm nay đều." {
		t.Fatalf("inserted %q", none.SituationAnalysis)
	}
	subject := &CoachStructuredOutput{
		SituationAnalysis: "Hôm nay da mày ổn. Da mày sẽ ổn.",
		AvoidOrPatch:      []string{"Nhớ mày nghỉ."},
		ConcernAlignment:  "Da mày đang dầu.",
	}
	sanitizeWebCoachOutput(subject, qa)
	if subject.AvoidOrPatch[0] != "Nhớ mày nghỉ." {
		t.Fatalf("subject dropped %q", subject.AvoidOrPatch[0])
	}
	if strings.Contains(subject.ConcernAlignment, "bạn") || strings.Contains(subject.SituationAnalysis, "bạn") {
		t.Fatalf("substituted bạn %q %q", subject.SituationAnalysis, subject.ConcernAlignment)
	}
	en := &CoachStructuredOutput{SituationAnalysis: clearSit, ConcernAlignment: clearCon}
	sanitizeWebCoachOutput(en, webQA{Locale: "en"})
	if en.SituationAnalysis != clearSit || en.ConcernAlignment != clearCon {
		t.Fatalf("en changed %q %q", en.SituationAnalysis, en.ConcernAlignment)
	}
	android := &CoachStructuredOutput{SituationAnalysis: clearSit, ConcernAlignment: acneCon}
	applyCoachVoiceGuard(android, "android", qa)
	if strings.Contains(android.SituationAnalysis, "thói quen chăm da") || !strings.Contains(android.SituationAnalysis, "xây dựng routine chăm sóc") {
		t.Fatalf("android rewrote the routine phrase %q", android.SituationAnalysis)
	}
	if !strings.Contains(android.ConcernAlignment, "dưỡng ẩm trước, để") {
		t.Fatalf("android dropped trước %q", android.ConcernAlignment)
	}
}

func TestWebVI_FirstCheckInAndCareTitles(t *testing.T) {
	const before = "Hôm nay da mày trông khá ổn — mịn màng, không thấy mụn hay đỏ nổi bật ở bất kỳ vùng nào. Trán, hai bên má, và vùng cằm đều có vẻ yên tĩnh. Hôm nay là lần check-in đầu, tao chỉ nhìn ngày hôm nay."
	const after = "Hôm nay da mày trông khá ổn — mịn màng, không thấy mụn hay đỏ nổi bật ở bất kỳ vùng nào. Trán, hai bên má, và vùng cằm đều có vẻ yên tĩnh. Lần đầu soi nên tao chỉ dựa vào ảnh hôm nay thôi nha."
	qa := webQA{Locale: "vi"}
	out := &CoachStructuredOutput{
		SituationAnalysis: before,
		CareSuggestions: []CoachCareSuggestion{
			{Slot: "morning", Step: "Rửa mặt dịu", Why: "Bắt đầu ngày với làn da sạch, không kích ứng."},
			{Step: "Chống nắng SPF50"},
			{Slot: "evening", Step: "Rửa mặt dịu", Why: "Loại bỏ bụi và dầu trong ngày, chuẩn bị da cho đêm."},
			{Step: "Dưỡng ẩm"},
		},
	}
	sanitizeWebCoachOutput(out, qa)
	if out.SituationAnalysis != after {
		t.Fatalf("situation\n got %q\nwant %q", out.SituationAnalysis, after)
	}
	if strings.Contains(out.SituationAnalysis, "mày") && strings.Count(strings.ToLower(out.SituationAnalysis), "mày") != 1 {
		t.Fatalf("mày count in %q", out.SituationAnalysis)
	}
	if out.CareSuggestions[0].Step != "Rửa mặt dịu (sáng)" || out.CareSuggestions[0].Slot != "morning" {
		t.Fatalf("morning %+v", out.CareSuggestions[0])
	}
	if out.CareSuggestions[1].Step != "Chống nắng SPF50" {
		t.Fatalf("spf %q", out.CareSuggestions[1].Step)
	}
	if out.CareSuggestions[2].Step != "Rửa mặt dịu (tối)" || out.CareSuggestions[2].Slot != "evening" {
		t.Fatalf("evening %+v", out.CareSuggestions[2])
	}
	if out.CareSuggestions[3].Step != "Dưỡng ẩm" {
		t.Fatalf("moist %q", out.CareSuggestions[3].Step)
	}

	unknown := &CoachStructuredOutput{CareSuggestions: []CoachCareSuggestion{
		{Step: "Rửa mặt dịu", Why: "Bắt đầu ngày với làn da sạch, không kích ứng."},
		{Step: "Rửa mặt dịu", Why: "Loại bỏ bụi và dầu trong ngày, chuẩn bị da cho đêm."},
	}}
	sanitizeWebCoachOutput(unknown, qa)
	if unknown.CareSuggestions[0].Step != "Rửa mặt dịu" || unknown.CareSuggestions[1].Step != "Rửa mặt dịu" {
		t.Fatalf("unknown order changed %#v", unknown.CareSuggestions)
	}

	byRoutine := &CoachStructuredOutput{
		RoutineHints: []string{"Sáng: Rửa mặt dịu", "Tối: Rửa mặt dịu"},
		CareSuggestions: []CoachCareSuggestion{
			{Step: "Rửa mặt dịu"},
			{Step: "Rửa mặt dịu"},
		},
	}
	sanitizeWebCoachOutput(byRoutine, qa)
	if byRoutine.CareSuggestions[0].Step != "Rửa mặt dịu (sáng)" || byRoutine.CareSuggestions[1].Step != "Rửa mặt dịu (tối)" {
		t.Fatalf("routine order %#v", byRoutine.CareSuggestions)
	}

	en := &CoachStructuredOutput{SituationAnalysis: before}
	sanitizeWebCoachOutput(en, webQA{Locale: "en"})
	if en.SituationAnalysis != before {
		t.Fatalf("en %q", en.SituationAnalysis)
	}
	first := CheckInCoachFacts{CountKnown: true, TotalIncludesToday: 1}
	web := &CoachStructuredOutput{SummaryNotes: "Kiên trì vậy mai sẽ khác."}
	if !ApplyCheckInAdviceGuard(web, first, "") {
		t.Fatal("web first check-in was not rewritten")
	}
	if web.SummaryNotes != "Lần đầu soi nên tao chỉ dựa vào ảnh hôm nay thôi nha." {
		t.Fatalf("web fallback %q", web.SummaryNotes)
	}
}

func TestWebVI_ImprovementVoice(t *testing.T) {
	qa := webQA{Locale: "vi"}
	real := &CoachStructuredOutput{Improvements: []struct {
		Tip string `json:"tip"`
		Why string `json:"why"`
	}{{
		Why: "Cứ giữ nhịp khi da đang ổn và mình muốn giữ nó như vậy.",
		Tip: "Bạn nên thoa kem mỏng.",
	}}}
	sanitizeWebCoachOutput(real, qa)
	if real.Improvements[0].Why != "Cứ giữ nhịp khi da đang ổn và muốn giữ da như vậy." {
		t.Fatalf("why %q", real.Improvements[0].Why)
	}
	if strings.Contains(real.Improvements[0].Why, "mày") || strings.Contains(real.Improvements[0].Why, "mình") {
		t.Fatalf("why voice %q", real.Improvements[0].Why)
	}
	if real.Improvements[0].Tip != "Nên thoa kem mỏng." {
		t.Fatalf("tip %q", real.Improvements[0].Tip)
	}

	only := &CoachStructuredOutput{
		SituationAnalysis: "Hôm nay da mày ổn.",
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Da mày sẽ ổn."}, {Why: "Da mày đang dầu."}},
	}
	sanitizeWebCoachOutput(only, qa)
	if only.Improvements[0].Tip != "Da sẽ ổn." || only.Improvements[1].Why != "Da đang dầu." {
		t.Fatalf("improvements counted? %#v", only.Improvements)
	}
	if only.SituationAnalysis != "Hôm nay da mày ổn." {
		t.Fatalf("opening %q", only.SituationAnalysis)
	}

	hidden := &CoachStructuredOutput{
		SituationAnalysis: "Hôm nay da đang ổn.",
		CareSuggestions:   []CoachCareSuggestion{{Step: "Dưỡng ẩm", Why: "Giữ ẩm."}},
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Da mày sẽ ổn."}, {Why: "Da mày đang dầu."}},
	}
	sanitizeWebCoachOutput(hidden, qa)
	if hidden.Improvements[0].Tip != "Da mày sẽ ổn." || hidden.Improvements[1].Why != "Da mày đang dầu." {
		t.Fatalf("hidden improvements were capped %#v", hidden.Improvements)
	}
	if hidden.SituationAnalysis != "Hôm nay da của mày đang ổn." {
		t.Fatalf("hidden mày blocked the opening %q", hidden.SituationAnalysis)
	}

	en := &CoachStructuredOutput{Improvements: []struct {
		Tip string `json:"tip"`
		Why string `json:"why"`
	}{{Why: "Cứ giữ nhịp khi da đang ổn và mình muốn giữ nó như vậy."}}}
	sanitizeWebCoachOutput(en, webQA{Locale: "en"})
	if en.Improvements[0].Why != "Cứ giữ nhịp khi da đang ổn và mình muốn giữ nó như vậy." {
		t.Fatalf("en %q", en.Improvements[0].Why)
	}
}

func TestQA_AndroidPromptHashUnchanged(t *testing.T) {
	if androidPromptHash("intermediate") != goldenAndroidIntermediateHash {
		t.Fatalf("android intermediate hash changed: %s", androidPromptHash("intermediate"))
	}
	if androidPromptHash("beginner") != goldenAndroidBeginnerHash {
		t.Fatalf("android beginner hash changed: %s", androidPromptHash("beginner"))
	}
}

func TestResolveCamera_DefaultFront(t *testing.T) {
	if ResolveCamera(dto.StoredPhotoContext{}) != "front" {
		t.Fatal("missing camera should default to front")
	}
	rear := dto.StoredPhotoContext{Images: []dto.PhotoMetaImage{{Camera: "rear"}}}
	if ResolveCamera(rear) != "rear" {
		t.Fatal("rear")
	}
	raw, err := dto.ParsePhotoContext(`[{"kind":"full_face","camera":"front"}]`, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if dto.DecodePhotoContext(raw).Images[0].Camera != "front" {
		t.Fatalf("stored %#v", dto.DecodePhotoContext(raw))
	}
	_ = domain.RefreshClientWeb
}
