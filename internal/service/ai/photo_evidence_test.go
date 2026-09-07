package ai

import "testing"

func TestClassifyCheckInPhotoEvidence(t *testing.T) {
	t.Parallel()
	skip := ClassifyCheckInPhotoEvidence("skipped_no_photo", "")
	if skip.Kind != PhotoEvidenceSkip || skip.Limited {
		t.Fatalf("skip: %+v", skip)
	}

	ok := ClassifyCheckInPhotoEvidence("ok", `{"photo_assessment":{"limitations":""},"visible_observations":["má: vài nốt"],"uncertainty_note":""}`)
	if ok.Kind != PhotoEvidenceOK || ok.Limited {
		t.Fatalf("ok: %+v", ok)
	}

	limited := ClassifyCheckInPhotoEvidence("ok", `{"photo_assessment":{"limitations":"ảnh hơi mờ, thiếu sáng"},"visible_observations":[]}`)
	if limited.Kind != PhotoEvidenceLimited || !limited.Limited || limited.Note == "" {
		t.Fatalf("limited: %+v", limited)
	}

	failed := ClassifyCheckInPhotoEvidence("unavailable", "")
	if failed.Kind != PhotoEvidenceLimited || !failed.Limited {
		t.Fatalf("unavailable: %+v", failed)
	}
}

func TestPhotoEvidencePromptBlock(t *testing.T) {
	t.Parallel()
	skip := PhotoEvidencePromptBlock(CheckInPhotoEvidence{Kind: PhotoEvidenceSkip}, "vi")
	mustContain(t, skip, "PHOTO_EVIDENCE: skip")
	mustContain(t, skip, "chưa chắc")

	lim := PhotoEvidencePromptBlock(CheckInPhotoEvidence{Kind: PhotoEvidenceLimited, Note: "ảnh mờ", Limited: true}, "vi")
	mustContain(t, lim, "PHOTO_EVIDENCE: limited")
	mustContain(t, lim, "ảnh mờ")
	mustContain(t, lim, "Chụp lại")

	ok := PhotoEvidencePromptBlock(CheckInPhotoEvidence{Kind: PhotoEvidenceOK}, "vi")
	mustContain(t, ok, "PHOTO_EVIDENCE: ok")
	mustContain(t, ok, "Speak plainly")
}

func TestApplyPhotoEvidenceToScores(t *testing.T) {
	t.Parallel()
	labels := map[string]any{}
	ApplyPhotoEvidenceToScores(labels, CheckInPhotoEvidence{
		Kind:    PhotoEvidenceLimited,
		Note:    "ảnh tối",
		Limited: true,
	})
	if labels["photo_evidence"] != PhotoEvidenceLimited {
		t.Fatalf("kind=%v", labels["photo_evidence"])
	}
	if labels["photo_limited"] != true {
		t.Fatalf("limited=%v", labels["photo_limited"])
	}
	if labels["photo_limited_note"] != "ảnh tối" {
		t.Fatalf("note=%v", labels["photo_limited_note"])
	}
}

func TestBuildSkinCheckCoachUserMessage_PhotoEvidence(t *testing.T) {
	t.Parallel()
	skip := buildSkinCheckCoachUserMessage(nil, nil, "", "", "skipped_no_photo", "TODAY tags only")
	mustContain(t, skip, "PHOTO_EVIDENCE: skip")

	blur := `{"photo_assessment":{"limitations":"ảnh hơi mờ, thiếu sáng"},"visible_observations":["má: nốt"]}`
	lim := buildSkinCheckCoachUserMessage(nil, nil, "", blur, "ok", "TODAY")
	mustContain(t, lim, "PHOTO_EVIDENCE: limited")
	mustContain(t, lim, "VISION_SUMMARY_JSON")
}
