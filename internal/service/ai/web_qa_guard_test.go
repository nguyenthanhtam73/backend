package ai

import (
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

	cmp := &CoachStructuredOutput{SituationAnalysis: "So với lần trước, vùng này trông sưng hơn."}
	sanitizeWebCoachOutput(cmp, webQA{GuardComparison: true, PreviousComparable: false})
	if strings.TrimSpace(cmp.SituationAnalysis) != "" {
		t.Fatalf("comparison survived %q", cmp.SituationAnalysis)
	}
	trunc := &CoachStructuredOutput{SituationAnalysis: "Má trái kém lần trước."}
	sanitizeWebCoachOutput(trunc, webQA{GuardComparison: true, PreviousComparable: false, SwapCheeks: true})
	if strings.TrimSpace(trunc.SituationAnalysis) != "" || strings.Contains(trunc.SituationAnalysis, "má phải") {
		t.Fatalf("fragment %q", trunc.SituationAnalysis)
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
