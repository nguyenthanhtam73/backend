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
