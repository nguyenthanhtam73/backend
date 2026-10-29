package ai

import (
	"encoding/json"
	"strings"
)

// Photo-note tags are the only values PhotoNoteTag returns. They describe why
// a check-in photo was hard to read. The raw limitations / uncertainty text
// is never returned.
const (
	photoNoteTagBlur       = "blur"
	photoNoteTagDark       = "dark"
	photoNoteTagCrop       = "crop"
	photoNoteTagShadow     = "shadow"
	photoNoteTagUnreadable = "unreadable"
	photoNoteTagOther      = "other"
	photoNoteTagNone       = "none"
)

// photoNoteTagRules is the CheckInVisionPhotoLimited keyword list, grouped so
// the first matching group wins. Priority is blur, dark, crop, shadow, then
// unreadable. "lighting", "low light", and "overexpos" sit with dark: they are
// the same badly-lit needles the limited check already uses.
var photoNoteTagRules = []struct {
	tag     string
	needles []string
}{
	{photoNoteTagBlur, []string{"mờ", "mo ", "blur"}},
	{photoNoteTagDark, []string{"tối", "toi ", "thiếu sáng", "dark", "lighting", "low light", "overexpos"}},
	{photoNoteTagCrop, []string{"crop", "cắt"}},
	{photoNoteTagShadow, []string{"shadow", "bóng"}},
	{photoNoteTagUnreadable, []string{"khó đọc", "hard to", "cannot", "không đọc"}},
}

// PhotoNoteTag classifies photo_assessment.limitations and uncertainty_note.
// Empty text is none. Text that names a photo problem is blur, dark, crop,
// shadow, or unreadable. Any other non-empty text is other.
//
// The scan string is the two fields joined by one space, then lowercased —
// the same string CheckInVisionPhotoLimited searches.
func PhotoNoteTag(limitations, uncertaintyNote string) string {
	lim := strings.TrimSpace(limitations)
	unc := strings.TrimSpace(uncertaintyNote)
	if lim == "" && unc == "" {
		return photoNoteTagNone
	}
	low := strings.ToLower(lim + " " + unc)
	for _, rule := range photoNoteTagRules {
		for _, needle := range rule.needles {
			if strings.Contains(low, needle) {
				return rule.tag
			}
		}
	}
	return photoNoteTagOther
}

// PhotoNoteTagFromVision reads limitations and uncertainty_note from check-in
// vision JSON and returns PhotoNoteTag. Invalid JSON, and notes that live on
// any other field, are none. The raw JSON is not returned.
func PhotoNoteTagFromVision(raw string) string {
	lim, unc, ok := visionPhotoNotes(raw)
	if !ok {
		return photoNoteTagNone
	}
	return PhotoNoteTag(lim, unc)
}

// visionPhotoNotes extracts the two photo-note fields. ok is false when the
// payload is empty or not the vision object.
func visionPhotoNotes(raw string) (limitations, uncertainty string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", false
	}
	var payload checkInVisionPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", "", false
	}
	return payload.PhotoAssessment.Limitations, payload.UncertaintyNote, true
}

func photoNoteHasProblemKeyword(limitations, uncertainty string) bool {
	switch PhotoNoteTag(limitations, uncertainty) {
	case photoNoteTagBlur, photoNoteTagDark, photoNoteTagCrop, photoNoteTagShadow, photoNoteTagUnreadable:
		return true
	default:
		return false
	}
}

func photoNoteKeywordNeedles() []string {
	out := make([]string, 0, 18)
	for _, rule := range photoNoteTagRules {
		out = append(out, rule.needles...)
	}
	return out
}
