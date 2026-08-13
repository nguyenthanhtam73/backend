package ai

import "strings"

// morphology_classify.go — CLASSIFICATION AS CODE.
//
// Vision models are good at reporting low-level features ("raised", "skin-colored",
// "no redness", "rough surface") and bad at staying consistent when they must also
// pick a group name in the same breath — they commit to a label, then bend the prose
// to justify it. So the split is:
//
//	vision pass  → MorphologyFeatures (low-level, per region)
//	Go (here)    → MorphologyGroup + confidence + what is still missing
//
// The decision tree mirrors VisionMorphologyRules() but is unit-testable without any
// API call, and cannot drift when prompts are edited. Pipelines map the resulting
// group onto their own label space (admin enums, onboarding concern_types, bullets).

// Feature value vocabularies (all lowercase; "" or unknown = not observed).
const (
	RaisedYes     = "raised"
	RaisedFlat    = "flat"
	RaisedMixed   = "mixed"
	RaisedUnknown = "unknown"

	ColorSkin    = "skin"
	ColorWhite   = "white"
	ColorBrown   = "brown"
	ColorRed     = "red"
	ColorBlack   = "black"
	ColorUnknown = "unknown"

	RedNone     = "none"
	RedMild     = "mild"
	RedModerate = "moderate"
	RedSevere   = "severe"
	RedUnknown  = "unknown"

	PusNone       = "none"
	PusWhiteheads = "whiteheads"
	PusVisible    = "pus"
	PusUnknown    = "unknown"

	ShapeRoundSmooth = "round_smooth"
	ShapeRoughUneven = "rough_uneven"
	ShapeStalkedSoft = "stalked_soft"
	ShapeOpenPore    = "open_pore"
	ShapeCrease      = "crease"
	ShapeUnknown     = "unknown"

	DensityFew      = "few"
	DensityModerate = "moderate"
	DensityDense    = "dense"
	DensityUnknown  = "unknown"
)

// MorphologyFeatures is one region's low-level photo observation.
type MorphologyFeatures struct {
	// Region uses the shared region ids (cheeks, forehead, nose, chin, neck, other…).
	Region string
	// Raised: raised | flat | mixed | unknown — needs oblique light to read reliably.
	Raised string
	// Color: skin | white | brown | red | black | unknown.
	Color string
	// Red: none | mild | moderate | severe | unknown (background redness of the area).
	Red string
	// Pus: none | whiteheads | pus | unknown.
	Pus string
	// Shape: round_smooth | rough_uneven | stalked_soft | open_pore | crease | unknown.
	Shape string
	// Density: few | moderate | dense | unknown.
	Density string
	// Swollen is true when individual bumps look swollen/inflamed (not just pink skin).
	Swollen bool
}

// MorphologyGroup is the plain-language group the whole app keys care advice off.
type MorphologyGroup string

const (
	GroupInflamedAcne       MorphologyGroup = "inflamed_acne"              // mụn viêm
	GroupPustules           MorphologyGroup = "pustules"                   // mụn có mủ
	GroupClosedComedones    MorphologyGroup = "closed_comedones"           // mụn ẩn thuần
	GroupComedonesIrritated MorphologyGroup = "closed_comedones_irritated" // mụn ẩn + kích ứng
	GroupMiliaLike          MorphologyGroup = "milia_like"                 // mụn ẩn hoặc milia
	GroupRoughTexture       MorphologyGroup = "rough_texture"              // sần sùi / texture không đều
	GroupOpenComedones      MorphologyGroup = "open_comedones"             // mụn cồi / đầu đen
	GroupPigment            MorphologyGroup = "pigment"                    // thâm / sắc tố
	GroupSkinTag            MorphologyGroup = "skin_tag"                   // mụn thịt
	GroupNeckCrease         MorphologyGroup = "neck_crease"                // nếp gấp cổ
	GroupUnknown            MorphologyGroup = "unknown"
)

// Confidence levels for a classification.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Missing-cue ids — what would settle an ambiguous case (drives clarify questions).
const (
	CueTouchFirmness = "touch_firmness" // sờ cứng như hạt vs mềm
	CueStalk         = "stalk"          // có cuống / dẹt không
	CueDuration      = "duration"       // bao lâu rồi, có thay đổi không
	CuePain          = "pain"           // đau / rát / ngứa
	CueObliqueLight  = "oblique_light"  // ảnh ánh sáng xiên để đọc nổi/phẳng
	CueRegionPhoto   = "region_photo"   // ảnh rõ vùng đang hỏi
)

// MorphologyVerdict is the classification result for one region.
type MorphologyVerdict struct {
	Group      MorphologyGroup
	Confidence string
	// NeedsMoreInfo is true when the photo alone cannot separate look-alike groups.
	NeedsMoreInfo bool
	// MissingCues lists cue ids that would settle it (see Cue* constants).
	MissingCues []string
	// Alternatives are the other plausible groups when NeedsMoreInfo is true.
	Alternatives []MorphologyGroup
}

var faceRegions = map[string]struct{}{
	"cheeks": {}, "cheek": {}, "forehead": {}, "nose": {}, "chin": {},
	"t_zone": {}, "jawline": {}, "jaw": {}, "perioral": {}, "temples": {}, "under_eyes": {},
}

var tagFriendlyRegions = map[string]struct{}{
	"neck": {}, "other": {}, "axilla": {}, "eyelid": {}, "under_eyes": {},
}

// IsFaceRegion reports whether a region id is on the face (not neck/body).
func IsFaceRegion(region string) bool {
	_, ok := faceRegions[normLower(region)]
	return ok
}

func isTagFriendlyRegion(region string) bool {
	_, ok := tagFriendlyRegions[normLower(region)]
	return ok
}

func fv(v, fallback string) string {
	v = normLower(v)
	if v == "" {
		return fallback
	}
	return v
}

// ClassifyMorphology turns low-level features into a group + honest confidence.
//
// It never guesses between look-alikes: when the photo cannot separate them it
// returns the safest group with NeedsMoreInfo + the cues that would settle it,
// so callers can ask instead of shipping a wrong group into care advice.
func ClassifyMorphology(f MorphologyFeatures) MorphologyVerdict {
	raised := fv(f.Raised, RaisedUnknown)
	color := fv(f.Color, ColorUnknown)
	red := fv(f.Red, RedUnknown)
	pus := fv(f.Pus, PusUnknown)
	shape := fv(f.Shape, ShapeUnknown)
	region := normLower(f.Region)

	hasPus := pus == PusWhiteheads || pus == PusVisible
	redClear := red == RedModerate || red == RedSevere
	redAny := redClear || red == RedMild
	noRed := red == RedNone

	// Neck creases: flat crease lines, no bumps at all.
	if shape == ShapeCrease && !hasPus && !redClear {
		return MorphologyVerdict{Group: GroupNeckCrease, Confidence: ConfidenceHigh}
	}

	// Open comedones: visible open pore / blackhead.
	if shape == ShapeOpenPore || (color == ColorBlack && raised != RaisedFlat) {
		return MorphologyVerdict{Group: GroupOpenComedones, Confidence: ConfidenceHigh}
	}

	// Flat brown/grey = pigment. Never a bump group.
	// Flat + skin-coloured is NOT pigment (nothing is actually discoloured) — it falls
	// through so the verdict ends up honest rather than inventing dark marks.
	if raised == RaisedFlat && color == ColorBrown && !hasPus {
		return MorphologyVerdict{Group: GroupPigment, Confidence: ConfidenceHigh}
	}

	// Pus/whiteheads on a raised, red bump = pustules.
	if hasPus && (redAny || f.Swollen) {
		return MorphologyVerdict{Group: GroupPustules, Confidence: ConfidenceHigh}
	}

	// Clearly red + swollen bumps = inflamed acne.
	if f.Swollen && redAny {
		return MorphologyVerdict{Group: GroupInflamedAcne, Confidence: ConfidenceHigh}
	}
	if redClear && raised == RaisedYes && !hasPus {
		return MorphologyVerdict{Group: GroupInflamedAcne, Confidence: ConfidenceMedium}
	}

	// Raised, skin-coloured, no pus — the look-alike cluster that used to get mislabeled.
	if raised == RaisedYes || raised == RaisedMixed {
		// Redness decides calm-first vs gentle care, so an unreadable redness cue is the
		// one doubt worth raising with the user (see withRednessDoubt).
		rednessUnreadable := red == RedUnknown

		if redAny && !noRed {
			// Tiny bumps on a pink base: both things are true, say both.
			return MorphologyVerdict{Group: GroupComedonesIrritated, Confidence: ConfidenceMedium}
		}

		// Soft stalked/flat-topped bumps off the face = skin tags.
		if shape == ShapeStalkedSoft {
			if isTagFriendlyRegion(region) {
				return withRednessDoubt(MorphologyVerdict{Group: GroupSkinTag, Confidence: ConfidenceHigh}, rednessUnreadable)
			}
			// Stalked shape but on the face — contradictory, do not claim skin tag.
			return withRednessDoubt(MorphologyVerdict{
				Group:         GroupMiliaLike,
				Confidence:    ConfidenceLow,
				NeedsMoreInfo: true,
				MissingCues:   []string{CueTouchFirmness, CueStalk, CueDuration},
				Alternatives:  []MorphologyGroup{GroupSkinTag, GroupClosedComedones},
			}, rednessUnreadable)
		}

		if shape == ShapeRoughUneven {
			// Rough uneven surface wins over "just milia" — texture, not red bumps.
			return withRednessDoubt(MorphologyVerdict{Group: GroupRoughTexture, Confidence: ConfidenceHigh}, rednessUnreadable)
		}

		if shape == ShapeRoundSmooth {
			if IsFaceRegion(region) {
				// Photos cannot separate milia from closed comedones — touch/duration can.
				// Both lead to the same care, so ShouldAskUser keeps this quiet for users.
				return withRednessDoubt(MorphologyVerdict{
					Group:         GroupMiliaLike,
					Confidence:    ConfidenceMedium,
					NeedsMoreInfo: true,
					MissingCues:   []string{CueTouchFirmness, CueDuration},
					Alternatives:  []MorphologyGroup{GroupClosedComedones},
				}, rednessUnreadable)
			}
			if isTagFriendlyRegion(region) {
				return withRednessDoubt(MorphologyVerdict{
					Group:         GroupSkinTag,
					Confidence:    ConfidenceLow,
					NeedsMoreInfo: true,
					MissingCues:   []string{CueTouchFirmness, CueStalk},
					Alternatives:  []MorphologyGroup{GroupMiliaLike},
				}, rednessUnreadable)
			}
		}

		// Raised + skin/white, shape unread: closed comedones is the safe read.
		if color == ColorSkin || color == ColorWhite || color == ColorUnknown {
			cues := []string{CueObliqueLight, CueTouchFirmness}
			alts := []MorphologyGroup{GroupMiliaLike, GroupRoughTexture}
			if !IsFaceRegion(region) {
				alts = append(alts, GroupSkinTag)
				cues = append(cues, CueStalk)
			}
			return withRednessDoubt(MorphologyVerdict{
				Group:         GroupClosedComedones,
				Confidence:    ConfidenceLow,
				NeedsMoreInfo: true,
				MissingCues:   cues,
				Alternatives:  alts,
			}, rednessUnreadable)
		}
	}

	// Rough surface without a readable raise still means texture.
	if shape == ShapeRoughUneven && !hasPus && !redClear {
		return MorphologyVerdict{Group: GroupRoughTexture, Confidence: ConfidenceMedium}
	}

	if color == ColorBrown && !hasPus {
		return MorphologyVerdict{Group: GroupPigment, Confidence: ConfidenceMedium}
	}

	if redClear {
		return MorphologyVerdict{Group: GroupInflamedAcne, Confidence: ConfidenceLow, NeedsMoreInfo: true, MissingCues: []string{CueObliqueLight, CuePain}}
	}

	return MorphologyVerdict{
		Group:         GroupUnknown,
		Confidence:    ConfidenceLow,
		NeedsMoreInfo: true,
		MissingCues:   []string{CueObliqueLight, CueRegionPhoto, CueTouchFirmness},
	}
}

// MorphologyGroupLabel returns the plain user-facing group name.
func MorphologyGroupLabel(g MorphologyGroup, locale string) string {
	en := strings.EqualFold(strings.TrimSpace(locale), "en")
	switch g {
	case GroupInflamedAcne:
		if en {
			return "inflamed breakouts"
		}
		return "mụn viêm"
	case GroupPustules:
		if en {
			return "whitehead breakouts"
		}
		return "mụn có mủ"
	case GroupClosedComedones:
		if en {
			return "closed comedones"
		}
		return "mụn ẩn"
	case GroupComedonesIrritated:
		if en {
			return "closed comedones with mild irritation"
		}
		return "mụn ẩn kèm kích ứng/viêm nhẹ"
	case GroupMiliaLike:
		if en {
			return "closed comedones or milia"
		}
		return "mụn ẩn hoặc milia"
	case GroupRoughTexture:
		if en {
			return "uneven, rough texture"
		}
		return "sần sùi / texture không đều"
	case GroupOpenComedones:
		if en {
			return "blackheads"
		}
		return "mụn cồi / đầu đen"
	case GroupPigment:
		if en {
			return "dark marks"
		}
		return "thâm / sắc tố"
	case GroupSkinTag:
		if en {
			return "skin tags"
		}
		return "mụn thịt"
	case GroupNeckCrease:
		if en {
			return "neck creases"
		}
		return "nếp gấp / nếp ngang cổ"
	default:
		if en {
			return "not clear from the photo"
		}
		return "chưa đọc rõ từ ảnh"
	}
}

// AdminConcernForGroup maps a group onto the admin attention_areas concern enum.
func AdminConcernForGroup(g MorphologyGroup) string {
	switch g {
	case GroupPustules:
		return "pustules"
	case GroupInflamedAcne:
		return "papules"
	case GroupClosedComedones, GroupMiliaLike, GroupOpenComedones:
		return "acne"
	case GroupComedonesIrritated:
		return "irritation"
	case GroupRoughTexture, GroupNeckCrease:
		return "texture"
	case GroupPigment:
		return "pigmentation"
	case GroupSkinTag:
		return "other"
	default:
		return ""
	}
}

// OnboardingConcernTypeForGroup maps a group onto onboarding concern_types.
func OnboardingConcernTypeForGroup(g MorphologyGroup) string {
	switch g {
	case GroupInflamedAcne, GroupPustules:
		return "inflammatory_acne"
	case GroupClosedComedones, GroupMiliaLike, GroupOpenComedones, GroupComedonesIrritated:
		return "comedones"
	case GroupRoughTexture:
		return "texture"
	case GroupNeckCrease:
		return "wrinkles"
	case GroupPigment:
		return "pih"
	default:
		return ""
	}
}

// CareDirection is the axis that actually changes what we tell someone.
//
// It exists so we only interrupt a user when the answer would change their routine.
// Milia, closed comedones, skin tags and rough texture all land on the same advice
// (gentle base, don't pick or cut, see a clinic if you want it removed), so asking
// "sờ cứng hay mềm?" to separate them buys nothing and just makes the app look unsure.
type CareDirection string

const (
	// CareCalmFirst: irritation present — soothe, hold off acids/retinoids.
	CareCalmFirst CareDirection = "calm_first"
	// CareGentle: gentle cleanse + moisturise + SPF, don't pick; clinic for removal.
	CareGentle           CareDirection = "gentle"
	CareUnknownDirection CareDirection = "unknown"
)

// CareDirectionForGroup maps a group onto the care axis.
func CareDirectionForGroup(g MorphologyGroup) CareDirection {
	if GroupImpliesRedness(g) {
		return CareCalmFirst
	}
	if g == GroupUnknown {
		return CareUnknownDirection
	}
	return CareGentle
}

// AlternativesChangeCare reports whether any look-alike would lead to different advice.
func AlternativesChangeCare(v MorphologyVerdict) bool {
	base := CareDirectionForGroup(v.Group)
	for _, alt := range v.Alternatives {
		if CareDirectionForGroup(alt) != base {
			return true
		}
	}
	return false
}

// ShouldAskUser reports whether a follow-up question is worth showing a user.
//
// The classifier stays fully honest in NeedsMoreInfo (the photo really cannot separate
// those groups); this is the narrower product question: would their answer change what
// they should do? Only then is it worth denting their confidence in the read.
func ShouldAskUser(v MorphologyVerdict) bool {
	return v.NeedsMoreInfo && AlternativesChangeCare(v)
}

// withRednessDoubt adds the one ambiguity that genuinely changes care: when the photo
// never says whether the area is irritated, the same bumps might need calming first.
func withRednessDoubt(v MorphologyVerdict, rednessUnreadable bool) MorphologyVerdict {
	if !rednessUnreadable || GroupImpliesRedness(v.Group) {
		return v
	}
	v.NeedsMoreInfo = true
	v.Alternatives = append(v.Alternatives, GroupComedonesIrritated)
	v.MissingCues = append(v.MissingCues, CuePain)
	if v.Confidence == ConfidenceHigh {
		v.Confidence = ConfidenceMedium
	}
	return v
}

// GroupImpliesRedness reports whether the group means active redness/inflammation,
// i.e. care advice must calm first and must not push acids.
func GroupImpliesRedness(g MorphologyGroup) bool {
	switch g {
	case GroupInflamedAcne, GroupPustules, GroupComedonesIrritated:
		return true
	default:
		return false
	}
}

// MorphologyClarifyQuestions renders the missing cues as short user-facing questions.
func MorphologyClarifyQuestions(v MorphologyVerdict, locale string) []string {
	if !v.NeedsMoreInfo || len(v.MissingCues) == 0 {
		return nil
	}
	en := strings.EqualFold(strings.TrimSpace(locale), "en")
	seen := make(map[string]struct{}, len(v.MissingCues))
	out := make([]string, 0, len(v.MissingCues))
	for _, cue := range v.MissingCues {
		if _, dup := seen[cue]; dup {
			continue
		}
		seen[cue] = struct{}{}
		var q string
		switch cue {
		case CueTouchFirmness:
			if en {
				q = "Do the bumps feel firm like a grain of sand, or soft?"
			} else {
				q = "Sờ vào thấy cứng như hạt cát, hay mềm?"
			}
		case CueStalk:
			if en {
				q = "Do they sit on a little stalk / feel floppy, or are they flat under the skin?"
			} else {
				q = "Nốt có cuống nhỏ (lắc lư được) hay nằm phẳng dưới da?"
			}
		case CueDuration:
			if en {
				q = "How long have they been there, and do they come and go?"
			} else {
				q = "Có bao lâu rồi, và có lên xuống theo đợt không?"
			}
		case CuePain:
			if en {
				q = "Are they sore, stinging, or itchy?"
			} else {
				q = "Có đau, rát hay ngứa không?"
			}
		case CueObliqueLight:
			if en {
				q = "Add one photo with light from the side — it shows whether bumps are raised or flat."
			} else {
				q = "Chụp thêm 1 ảnh ánh sáng chiếu ngang (đứng cạnh cửa sổ) — mới thấy nốt nổi cao hay phẳng."
			}
		case CueRegionPhoto:
			if en {
				q = "Add a clear close-up of the exact area you're asking about."
			} else {
				q = "Chụp thêm 1 ảnh close-up rõ đúng vùng mày đang hỏi."
			}
		}
		if q != "" {
			out = append(out, q)
		}
	}
	return out
}

// GroupFromCorrectedArea infers the ground-truth group from an operator-corrected
// attention area, for building the eval set out of real corrections.
//
// The corrected concern enum is the trustworthy signal — it is what the reviewer actually
// judged. Region + note only disambiguate enums that legitimately cover several groups
// (`acne` covers closed comedones / milia / blackheads; `texture` covers rough cheeks and
// neck creases). When the enum cannot pin a single group, ok is false and the caller
// should score against the concern instead of inventing a label.
func GroupFromCorrectedArea(concern, region, note string) (MorphologyGroup, bool) {
	c := normLower(concern)
	r := normLower(region)

	switch c {
	case "pustules":
		return GroupPustules, true
	case "papules":
		return GroupInflamedAcne, true
	case "pigmentation", "dark_spots":
		return GroupPigment, true
	case "texture":
		if proseHasPositiveCue(note, cueCrease...) || (r == "neck" && !proseHasPositiveCue(note, cueRough...)) {
			return GroupNeckCrease, true
		}
		return GroupRoughTexture, true
	case "other":
		if proseHasPositiveCue(note, cueSkinTag...) && isTagFriendlyRegion(r) {
			return GroupSkinTag, true
		}
		return GroupUnknown, false
	case "acne":
		if proseHasPositiveCue(note, cueOpenPore...) {
			return GroupOpenComedones, true
		}
		if proseHasPositiveCue(note, cueMilia...) {
			return GroupMiliaLike, true
		}
		if proseHasPositiveCue(note, cueTinyBumps...) {
			return GroupClosedComedones, true
		}
		return GroupUnknown, false
	case "irritation":
		if proseHasPositiveCue(note, cueTinyBumps...) {
			return GroupComedonesIrritated, true
		}
		return GroupUnknown, false
	default:
		// none / not_visible / redness / pores / dryness / oiliness carry no single group.
		return GroupUnknown, false
	}
}

// WorstConfidence returns the lowest confidence among the given levels.
func WorstConfidence(levels ...string) string {
	worst := ConfidenceHigh
	rank := map[string]int{ConfidenceHigh: 0, ConfidenceMedium: 1, ConfidenceLow: 2}
	for _, l := range levels {
		l = normLower(l)
		if _, ok := rank[l]; !ok {
			continue
		}
		if rank[l] > rank[worst] {
			worst = l
		}
	}
	return worst
}
