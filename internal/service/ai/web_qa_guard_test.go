package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

func TestQA_InventedUserNote(t *testing.T) {
	const in = "Mày ghi chú là mụn ẩn"
	if got := stripInventedUserNote(in); strings.Contains(got, "ghi chú") || strings.Contains(got, "mày ghi") {
		t.Fatalf("invented note stayed %q", got)
	}
	kept := stripInventedUserNote("Da ổn. Mày ghi chú là mụn ẩn.")
	if strings.Contains(kept, "ghi chú") || !strings.Contains(kept, "Da ổn") {
		t.Fatalf("mixed note %q", kept)
	}
	if got := stripInventedUserNote("Mày ghi da hôm nay ổn."); strings.Contains(strings.ToLower(got), "mày ghi") {
		t.Fatalf("mày ghi stayed %q", got)
	}
}

func TestQA_CameraFrontAndRear(t *testing.T) {
	if MapFrameCheek("left", "front") != "right" || MapFrameCheek("right", "front") != "left" {
		t.Fatal("front camera should flip")
	}
	if MapFrameCheek("left", "rear") != "left" || MapFrameCheek("right", "rear") != "right" {
		t.Fatal("rear camera should keep the image edge")
	}
	if MapFrameCheek("left", "") != "right" {
		t.Fatal("unknown camera defaults to a mirrored front selfie")
	}
	front := FlipFrameCheeks("Nốt ở má trái, zone left_cheek.")
	if !strings.Contains(front, "má phải") || !strings.Contains(front, "right_cheek") || strings.Contains(front, "má trái") {
		t.Fatalf("front flip %q", front)
	}
	if MapFrameCheek("left", "rear") != "left" {
		t.Fatal("rear prose and keys stay on the image edge")
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
	positive := softenSmoothSkinClaim("Da mịn màng, đây là mụn ẩn.")
	if strings.Contains(positive, "mụn ẩn") || !strings.Contains(positive, "có vài nốt nhỏ, chưa chắc") {
		t.Fatalf("positive %q", positive)
	}
	milia := softenSmoothSkinClaim("Da mịn màng, trông giống milia.")
	if strings.Contains(strings.ToLower(milia), "milia") {
		t.Fatalf("smooth skin still claims milia: %q", milia)
	}
	judged := replaceUncleanJudgement("Kem này làm da trông không sạch.")
	if strings.Contains(judged, "không sạch") || !strings.Contains(judged, "làm nốt dễ thấy hơn") {
		t.Fatalf("judging %q", judged)
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
	opening := "Có một nốt mới ở má."
	fields := []polishField{{text: &opening, opening: true}}
	balanceMay(fields)
	if *fields[0].text != "Tao thấy Có một nốt mới ở má." && *fields[0].text != "Tao thấy có một nốt mới ở má." {
		if !strings.Contains(*fields[0].text, "Tao thấy") || strings.Contains(*fields[0].text, "Tao thấy da hôm nay vậy đó") {
			t.Fatalf("new pimple voice %q", *fields[0].text)
		}
	}
	many := "Da mày đỏ. Da mày sưng. Da mày ngứa. Mày ơi đừng nặn."
	mfields := []polishField{{text: &many, opening: true}}
	balanceMay(mfields)
	if strings.Count(strings.ToLower(*mfields[0].text), "mày") > 2 {
		t.Fatalf("still too many mày: %q", *mfields[0].text)
	}
	if !strings.Contains(*mfields[0].text, "Mày ơi") && !strings.Contains(*mfields[0].text, "mày ơi") {
		t.Fatalf("cut the opener: %q", *mfields[0].text)
	}
	if strings.Contains(strings.ToLower(replaceChungTa("chúng ta cùng làm")), "chúng ta") {
		t.Fatal("chúng ta stayed")
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
