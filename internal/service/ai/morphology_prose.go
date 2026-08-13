package ai

import (
	"encoding/json"
	"strings"
)

// morphology_prose.go — bridge between free-text vision prose and the Go classifier.
//
// Vision passes today describe skin in prose instead of emitting MorphologyFeatures,
// so features are inferred from what the model actually wrote. That lets the same
// decision tree (morphology_classify.go) audit every pipeline's labels, and lets us
// fix contradictions ("không thấy đỏ sưng" + label "nốt đỏ sưng") before the labels
// reach routine/care logic, where a wrong group means wrong advice.

var negationMarkers = []string{"không", "khong", "chưa", "chua", " no ", " not ", "without", "khỏi"}

// splitProseClauses splits on sentence/comma boundaries only.
// It deliberately does NOT split on "hay"/"và" so "không thấy đỏ sưng hay mủ"
// keeps its negation attached to both cues.
func splitProseClauses(s string) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return nil
	}
	repl := strings.NewReplacer(".", "\n", ",", "\n", ";", "\n", "!", "\n", "?", "\n", "·", "\n", "—", "\n")
	parts := strings.Split(repl.Replace(s), "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func clauseIsNegated(clause string) bool {
	padded := " " + clause + " "
	for _, n := range negationMarkers {
		if strings.Contains(padded, n) {
			return true
		}
	}
	return false
}

// proseHasPositiveCue reports a cue stated affirmatively somewhere in the prose.
func proseHasPositiveCue(prose string, needles ...string) bool {
	for _, clause := range splitProseClauses(prose) {
		if clauseIsNegated(clause) {
			continue
		}
		for _, n := range needles {
			if strings.Contains(clause, n) {
				return true
			}
		}
	}
	return false
}

// proseHasNegatedCue reports a cue explicitly denied ("không thấy đỏ sưng").
func proseHasNegatedCue(prose string, needles ...string) bool {
	for _, clause := range splitProseClauses(prose) {
		if !clauseIsNegated(clause) {
			continue
		}
		for _, n := range needles {
			if strings.Contains(clause, n) {
				return true
			}
		}
	}
	return false
}

var (
	cueRedSwollen  = []string{"đỏ sưng", "sưng đỏ", "viêm đỏ", "mụn viêm", "do sung", "inflamed", "swollen"}
	cueRedMild     = []string{"ửng đỏ", "đỏ hồng", "hơi đỏ", "hồng", "ung do", "do hong", "redness", "pink"}
	cuePus         = []string{"mủ", "đầu trắng", "mu ", "dau trang", "pus", "whitehead"}
	cueMilia       = []string{"milia"}
	cueRough       = []string{"sần sùi", "gồ ghề", "san sui", "go ghe", "không mịn đều", "texture không đều", "rough", "uneven surface"}
	cueSkinTag     = []string{"mụn thịt", "mun thit", "skin tag"}
	cueStalked     = []string{"có cuống", "co cuong", "mềm", "stalk", "floppy"}
	cueRaised      = []string{"nổi cao", "nổi lên", "noi cao", "nhô", "raised", "nổi rõ"}
	cueTinyBumps   = []string{"nốt nhỏ", "li ti", "not nho", "mụn ẩn", "mun an", "tiny bump", "small bump", "closed comedone"}
	cueUnderSkin   = []string{"dưới da", "duoi da", "under the skin", "under-skin"}
	cueRoundSmooth = []string{"tròn", "mịn", "tron", "min", "round", "smooth"}
	cueFlatPigment = []string{"thâm", "sắc tố", "nâu", "tham", "sac to", "nau", "dark mark", "pigment", "brown"}
	cueFlat        = []string{"phẳng", "phang", "flat"}
	cueCrease      = []string{"nếp ngang", "nếp gấp", "nep ngang", "nep gap", "crease"}
	cueOpenPore    = []string{"đầu đen", "lỗ đen", "mụn cồi", "dau den", "lo den", "mun coi", "blackhead", "open pore"}
	cueSkinColored = []string{"màu da", "mau da", "skin-colored", "skin coloured", "skin colored"}
)

// MorphologyFeaturesFromProse infers low-level features from vision prose.
// Unreadable cues stay "unknown" so the classifier can flag NeedsMoreInfo instead
// of inventing certainty.
func MorphologyFeaturesFromProse(prose, region string) MorphologyFeatures {
	f := MorphologyFeatures{
		Region:  normLower(region),
		Raised:  RaisedUnknown,
		Color:   ColorUnknown,
		Red:     RedUnknown,
		Pus:     PusUnknown,
		Shape:   ShapeUnknown,
		Density: DensityUnknown,
	}
	if strings.TrimSpace(prose) == "" {
		return f
	}

	switch {
	case proseHasPositiveCue(prose, cueRedSwollen...):
		f.Red = RedModerate
		f.Swollen = true
	case proseHasPositiveCue(prose, cueRedMild...):
		f.Red = RedMild
	case proseHasNegatedCue(prose, cueRedSwollen...) || proseHasNegatedCue(prose, cueRedMild...):
		f.Red = RedNone
	}

	switch {
	case proseHasPositiveCue(prose, cuePus...):
		f.Pus = PusWhiteheads
	case proseHasNegatedCue(prose, cuePus...):
		f.Pus = PusNone
	}

	if proseHasPositiveCue(prose, cueRaised...) || proseHasPositiveCue(prose, cueTinyBumps...) {
		f.Raised = RaisedYes
	}
	if proseHasPositiveCue(prose, cueFlat...) && !proseHasPositiveCue(prose, cueRaised...) {
		f.Raised = RaisedFlat
	}

	if proseHasPositiveCue(prose, cueSkinColored...) {
		f.Color = ColorSkin
	} else if proseHasPositiveCue(prose, cueFlatPigment...) && f.Raised != RaisedYes {
		f.Color = ColorBrown
	}

	// Shape — most specific first; rough/uneven beats round+smooth (texture case wins).
	switch {
	case proseHasPositiveCue(prose, cueCrease...):
		f.Shape = ShapeCrease
	case proseHasPositiveCue(prose, cueOpenPore...):
		f.Shape = ShapeOpenPore
	case proseHasPositiveCue(prose, cueRough...):
		f.Shape = ShapeRoughUneven
	case proseHasPositiveCue(prose, cueSkinTag...) && proseHasPositiveCue(prose, cueStalked...):
		f.Shape = ShapeStalkedSoft
	case proseHasPositiveCue(prose, cueRoundSmooth...):
		f.Shape = ShapeRoundSmooth
	}

	if proseHasPositiveCue(prose, cueMilia...) && f.Shape == ShapeUnknown {
		f.Shape = ShapeRoundSmooth
	}
	if proseHasPositiveCue(prose, cueUnderSkin...) && f.Raised == RaisedUnknown {
		f.Raised = RaisedYes
	}

	switch {
	case proseHasPositiveCue(prose, "dày đặc", "day dac", "dense", "khá nhiều", "nhiều nốt"):
		f.Density = DensityDense
	case proseHasPositiveCue(prose, "vài nốt", "vai not", "a few", "thưa"):
		f.Density = DensityFew
	}

	return f
}

// ProseDeniesRedness reports prose that explicitly says there is no redness/swelling.
func ProseDeniesRedness(prose string) bool {
	if proseHasPositiveCue(prose, cueRedSwollen...) || proseHasPositiveCue(prose, cueRedMild...) {
		return false
	}
	return proseHasNegatedCue(prose, cueRedSwollen...) || proseHasNegatedCue(prose, cueRedMild...)
}

// ProseMentionsSkinTagOnFace reports the classic mislabel: face bumps called mụn thịt.
func ProseMentionsSkinTagOnFace(prose string) bool {
	if !proseHasPositiveCue(prose, cueSkinTag...) {
		return false
	}
	low := strings.ToLower(prose)
	face := strings.Contains(low, "má") || strings.Contains(low, "cheek") ||
		strings.Contains(low, "trán") || strings.Contains(low, "forehead") ||
		strings.Contains(low, "cằm") || strings.Contains(low, "chin")
	offFace := strings.Contains(low, "cổ") || strings.Contains(low, "neck") ||
		strings.Contains(low, "nách") || strings.Contains(low, "axilla") ||
		strings.Contains(low, "mí") || strings.Contains(low, "eyelid")
	return face && !offFace
}

// RewriteSkinTagOnFace replaces face "mụn thịt" wording with the correct group.
func RewriteSkinTagOnFace(s, locale string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	replacement := "trông giống mụn ẩn hoặc milia"
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		replacement = "look like closed comedones or milia"
	}
	out := s
	for _, phrase := range []string{
		"trông giống mụn thịt", "đây trông giống mụn thịt", "là mụn thịt", "mụn thịt",
		"look like skin tags", "looks like skin tags", "skin tags", "skin tag",
	} {
		out = replaceCaseInsensitive(out, phrase, replacement)
	}
	return out
}

func replaceCaseInsensitive(haystack, needle, replacement string) string {
	if needle == "" {
		return haystack
	}
	var b strings.Builder
	low := strings.ToLower(haystack)
	lowNeedle := strings.ToLower(needle)
	for {
		i := strings.Index(low, lowNeedle)
		if i < 0 {
			b.WriteString(haystack)
			return b.String()
		}
		b.WriteString(haystack[:i])
		b.WriteString(replacement)
		haystack = haystack[i+len(needle):]
		low = low[i+len(lowNeedle):]
	}
}

// ---------------------------------------------------------------------------
// Check-in vision JSON sanitizing (same guarantees as the admin aligner)
// ---------------------------------------------------------------------------

// checkInVisionPayload MUST mirror every field in VisionObservationSchemaBlock:
// SanitizeCheckInVisionJSON re-marshals from this struct, so any schema key missing here
// would be silently dropped before the coach ever sees it.
type checkInVisionPayload struct {
	PhotoAssessment struct {
		Lighting     string `json:"lighting"`
		AngleClarity string `json:"angle_clarity"`
		Limitations  string `json:"limitations"`
	} `json:"photo_assessment"`
	VisibleObservations []string `json:"visible_observations"`
	TextureAndOilCues   string   `json:"texture_and_oil_cues"`
	RednessCues         string   `json:"redness_or_discoloration_cues"`
	UncertaintyNote     string   `json:"uncertainty_note"`
}

// SanitizeCheckInVisionJSON fixes morphology mislabels in the check-in vision JSON
// before it reaches the coach, so care advice is not built on a wrong group.
// Returns the (possibly rewritten) JSON and whether anything changed.
func SanitizeCheckInVisionJSON(raw, locale string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw, false
	}
	var payload checkInVisionPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return raw, false
	}

	changed := false

	// Check each field on its own: joining them with spaces would let a "không …" in a
	// later field land in the same clause as an earlier cue and silently negate it.
	for i, b := range payload.VisibleObservations {
		if ProseMentionsSkinTagOnFace(b) {
			payload.VisibleObservations[i] = RewriteSkinTagOnFace(b, locale)
			changed = true
		}
	}
	if ProseMentionsSkinTagOnFace(payload.TextureAndOilCues) {
		payload.TextureAndOilCues = RewriteSkinTagOnFace(payload.TextureAndOilCues, locale)
		changed = true
	}

	// Bullets that deny redness must not also carry inflamed-bump wording.
	for i, b := range payload.VisibleObservations {
		if ProseDeniesRedness(b) && proseHasPositiveCue(b, "nốt đỏ sưng", "not do sung", "red swollen") {
			payload.VisibleObservations[i] = replaceCaseInsensitive(
				replaceCaseInsensitive(b, "nốt đỏ sưng", "nốt nhỏ nổi cao"),
				"red swollen bumps", "small raised bumps",
			)
			changed = true
		}
	}

	if !changed {
		return raw, false
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return raw, false
	}
	return string(out), true
}

// CheckInVisionPhotoLimited reports whether the vision pass flagged a photo problem
// (blurry / dark / cropped) plus the note explaining it.
func CheckInVisionPhotoLimited(raw string) (limited bool, note string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false, ""
	}
	var payload checkInVisionPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return false, ""
	}
	lim := strings.TrimSpace(payload.PhotoAssessment.Limitations)
	unc := strings.TrimSpace(payload.UncertaintyNote)
	note = lim
	if note == "" {
		note = unc
	}
	if note == "" {
		return false, ""
	}
	// Only treat it as limiting when it names a real photo problem.
	low := strings.ToLower(lim + " " + unc)
	for _, n := range []string{
		"mờ", "mo ", "tối", "toi ", "thiếu sáng", "crop", "cắt", "blur", "dark", "lighting",
		"shadow", "bóng", "khó đọc", "hard to", "cannot", "không đọc", "low light", "overexpos",
	} {
		if strings.Contains(low, n) {
			return true, note
		}
	}
	return false, ""
}

// RetakePhotoTips returns short retake instructions, including the oblique-light ask
// that makes raised-vs-flat readable at all.
func RetakePhotoTips(locale string) []string {
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		return []string{
			"Retake in daylight near a window — no filter, no flash.",
			"Add one shot with light coming from the side: that's the only way raised bumps show up.",
			"Fill the frame with the area you're asking about, and hold still so it isn't blurry.",
		}
	}
	return []string{
		"Chụp lại bằng ánh sáng ban ngày cạnh cửa sổ — không filter, không flash.",
		"Thêm 1 ảnh để ánh sáng chiếu ngang má: chỉ cách này mới thấy nốt nổi cao hay phẳng.",
		"Lấy khung sát vùng đang hỏi, giữ tay chắc cho ảnh khỏi mờ.",
	}
}
