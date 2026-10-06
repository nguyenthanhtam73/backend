package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestCheckInConfidence_PhotoEvidence(t *testing.T) {
	t.Parallel()
	level, needs, qs := CheckInConfidence(CheckInPhotoEvidence{Kind: PhotoEvidenceOK}, `{"zone_observations":[]}`, nil, "vi")
	if level != ConfidenceHigh || needs || len(qs) != 0 {
		t.Fatalf("ok: %s needs=%v qs=%v", level, needs, qs)
	}
	level, needs, qs = CheckInConfidence(CheckInPhotoEvidence{Kind: PhotoEvidenceLimited}, "", nil, "vi")
	if level != ConfidenceLow || !needs || len(qs) != len(RetakePhotoTips("vi")) {
		t.Fatalf("limited: %s needs=%v qs=%v", level, needs, qs)
	}
	level, needs, qs = CheckInConfidence(CheckInPhotoEvidence{Kind: PhotoEvidenceSkip}, `{"zone_observations":[{"zone":"chin","cue":"nốt nhỏ màu da nổi cao tròn mịn","severity":"mild"}]}`, nil, "vi")
	if level != ConfidenceLow || needs || len(qs) != 0 {
		t.Fatalf("skip: %s needs=%v qs=%v", level, needs, qs)
	}
}

func TestCheckInConfidence_LookAlikeAsksUntilAnswered(t *testing.T) {
	t.Parallel()
	vision := `{"zone_observations":[{"zone":"left_cheek","cue":"nốt nhỏ màu da nổi cao tròn mịn","severity":"mild"}]}`
	level, needs, qs := CheckInConfidence(CheckInPhotoEvidence{Kind: PhotoEvidenceOK}, vision, nil, "vi")
	if level == ConfidenceHigh {
		t.Fatalf("look-alike should not stay high, got %s", level)
	}
	if !needs || len(qs) == 0 {
		t.Fatal("look-alike should ask")
	}
	joined := strings.Join(qs, " ")
	if !strings.Contains(joined, "cứng như hạt cát") {
		t.Fatalf("missing firmness question: %v", qs)
	}
	if len(qs) > 3 {
		t.Fatalf("more than 3 questions: %v", qs)
	}

	answered := photoContextJSON(t, nil, &dto.SkinContextInput{Firmness: "firm", Duration: "months", Pain: "none"})
	_, needs, qs = CheckInConfidence(CheckInPhotoEvidence{Kind: PhotoEvidenceOK}, vision, answered, "vi")
	if needs || len(qs) != 0 {
		t.Fatalf("answered skin_context should clear questions, got %v", qs)
	}
}

func TestCheckInConfidence_CloseUpDropsFramingTip(t *testing.T) {
	t.Parallel()
	closeup := photoContextJSON(t, []dto.PhotoMetaImage{{Index: 0, Kind: dto.PhotoKindCloseup, Zone: "left_cheek"}}, nil)
	_, _, qs := CheckInConfidence(CheckInPhotoEvidence{Kind: PhotoEvidenceLimited}, "", closeup, "vi")
	joined := strings.Join(qs, " ")
	if strings.Contains(joined, "sát vùng") {
		t.Fatalf("close-up still asks to fill the frame: %v", qs)
	}
	if !strings.Contains(joined, "cửa sổ") {
		t.Fatalf("other retake tips missing: %v", qs)
	}
	_, _, en := CheckInConfidence(CheckInPhotoEvidence{Kind: PhotoEvidenceLimited}, "", closeup, "en")
	if strings.Contains(strings.Join(en, " "), "Fill the frame") {
		t.Fatalf("english framing tip kept: %v", en)
	}
}
