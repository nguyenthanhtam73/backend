package ai

import "strings"

// Photo evidence kinds persisted on skin_scores and exposed on analysis.coach.
// Frontend resolveCoachPhotoEvidence accepts skip/skipped/skipped_no_photo and limited.
const (
	PhotoEvidenceOK      = "ok"
	PhotoEvidenceSkip    = "skip"
	PhotoEvidenceLimited = "limited"
)

// CheckInPhotoEvidence is the check-in vision quality used by the coach prompt and FE banner.
type CheckInPhotoEvidence struct {
	Kind    string // ok | skip | limited
	Note    string
	Limited bool
}

// ClassifyCheckInPhotoEvidence maps visionStatus + raw vision JSON to skip/limited/ok.
//
//	skipped_no_photo / skipped / no photos → skip
//	vision ok but blur/dark/crop           → limited
//	vision failed or no key (photos exist) → limited
//	vision ok and readable                 → ok
func ClassifyCheckInPhotoEvidence(visionStatus, visionRaw string) CheckInPhotoEvidence {
	status := strings.TrimSpace(strings.ToLower(visionStatus))
	switch status {
	case "skipped_no_photo", "skipped", "no_photo", "skip":
		return CheckInPhotoEvidence{Kind: PhotoEvidenceSkip}
	case "ok":
		if limited, note := CheckInVisionPhotoLimited(visionRaw); limited {
			return CheckInPhotoEvidence{Kind: PhotoEvidenceLimited, Note: note, Limited: true}
		}
		return CheckInPhotoEvidence{Kind: PhotoEvidenceOK}
	default:
		// unavailable / no_openai_key / empty — photos were intended but not readable.
		if limited, note := CheckInVisionPhotoLimited(visionRaw); limited {
			return CheckInPhotoEvidence{Kind: PhotoEvidenceLimited, Note: note, Limited: true}
		}
		if status == "unavailable" || status == "no_openai_key" {
			return CheckInPhotoEvidence{
				Kind:    PhotoEvidenceLimited,
				Note:    "không đọc được ảnh lần này",
				Limited: true,
			}
		}
		return CheckInPhotoEvidence{Kind: PhotoEvidenceSkip}
	}
}

// PhotoEvidencePromptBlock is injected into the coach user message so v28 can
// speak plainly on ok photos and MUST hedge on skip/limited.
func PhotoEvidencePromptBlock(ev CheckInPhotoEvidence, locale string) string {
	var b strings.Builder
	b.WriteString("\n\nPHOTO_EVIDENCE: ")
	b.WriteString(ev.Kind)
	switch ev.Kind {
	case PhotoEvidenceSkip:
		b.WriteString("\nNo check-in photo this turn (skip-face / tags+notes only).")
		b.WriteString(" MUST say chưa chắc / không có ảnh in situation_analysis or concern_alignment.")
		b.WriteString(" Do not lock a morphology group. Do not invent photo details.")
	case PhotoEvidenceLimited:
		b.WriteString("\nThe photo limits what can be read.")
		if n := strings.TrimSpace(ev.Note); n != "" {
			b.WriteString(" PHOTO_LIMITED: ")
			b.WriteString(n)
		}
		b.WriteString(" MUST express uncertainty in ONE short clause (ảnh hạn chế / chưa chắc).")
		b.WriteString(" Keep the read cautious for the affected cue. Put a retake ask in routine_hints using these: ")
		b.WriteString(strings.Join(RetakePhotoTips(locale), " | "))
	default:
		b.WriteString("\nPhoto is readable enough. Speak plainly — no hedge spam.")
	}
	return b.String()
}

// ApplyPhotoEvidenceToScores writes photo_evidence / photo_limited / photo_limited_note
// onto the persisted skin_scores map (no DB migration).
func ApplyPhotoEvidenceToScores(labels map[string]any, ev CheckInPhotoEvidence) {
	if labels == nil {
		return
	}
	labels["photo_evidence"] = ev.Kind
	labels["photo_limited"] = ev.Limited
	if n := strings.TrimSpace(ev.Note); n != "" {
		labels["photo_limited_note"] = n
	}
}
