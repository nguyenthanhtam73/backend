package ai

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

// morphology_classify_test.go — the OFFLINE EVAL for photo morphology.
//
// Every case below is a labeled fixture: features (or real-world prose) in, expected
// group out. This is what makes "nhận xét chính xác hơn" measurable without paying for
// a vision call: change the rules and this prints exactly which groups regressed.

type featureCase struct {
	name string
	in   MorphologyFeatures
	want MorphologyGroup
	// wantNeedsInfo asserts the classifier admits it cannot separate look-alikes.
	wantNeedsInfo bool
	// wantAsksUser asserts whether that doubt is worth showing a user: only when the
	// answer would change the care direction.
	wantAsksUser bool
}

func featureEvalCases() []featureCase {
	return []featureCase{
		{
			name: "cheek raised skin-colored round smooth = milia-like (never skin tag)",
			in:   MorphologyFeatures{Region: "cheeks", Raised: RaisedYes, Color: ColorSkin, Red: RedNone, Pus: PusNone, Shape: ShapeRoundSmooth},
			want: GroupMiliaLike, wantNeedsInfo: true, wantAsksUser: false,
		},
		{
			name: "cheek raised + rough uneven surface = texture (never red bumps)",
			in:   MorphologyFeatures{Region: "cheeks", Raised: RaisedYes, Color: ColorSkin, Red: RedNone, Pus: PusNone, Shape: ShapeRoughUneven},
			want: GroupRoughTexture,
		},
		{
			name: "neck raised soft stalked = skin tag",
			in:   MorphologyFeatures{Region: "neck", Raised: RaisedYes, Color: ColorSkin, Red: RedNone, Pus: PusNone, Shape: ShapeStalkedSoft},
			want: GroupSkinTag,
		},
		{
			name: "stalked shape but on the face must NOT be a skin tag",
			in:   MorphologyFeatures{Region: "cheeks", Raised: RaisedYes, Color: ColorSkin, Red: RedNone, Pus: PusNone, Shape: ShapeStalkedSoft},
			want: GroupMiliaLike, wantNeedsInfo: true, wantAsksUser: false,
		},
		{
			name: "raised bumps with unreadable redness must ask (calm-first vs gentle differ)",
			in:   MorphologyFeatures{Region: "cheeks", Raised: RaisedYes, Color: ColorSkin, Pus: PusNone, Shape: ShapeRoundSmooth},
			want: GroupMiliaLike, wantNeedsInfo: true, wantAsksUser: true,
		},
		{
			name: "swollen + moderate red = inflamed acne",
			in:   MorphologyFeatures{Region: "cheeks", Raised: RaisedYes, Color: ColorRed, Red: RedModerate, Pus: PusNone, Swollen: true},
			want: GroupInflamedAcne,
		},
		{
			name: "whiteheads on red bumps = pustules",
			in:   MorphologyFeatures{Region: "chin", Raised: RaisedYes, Color: ColorWhite, Red: RedMild, Pus: PusWhiteheads},
			want: GroupPustules,
		},
		{
			name: "tiny raised bumps on pink base = comedones + irritation",
			in:   MorphologyFeatures{Region: "cheeks", Raised: RaisedYes, Color: ColorSkin, Red: RedMild, Pus: PusNone},
			want: GroupComedonesIrritated,
		},
		{
			name: "flat brown = pigment",
			in:   MorphologyFeatures{Region: "cheeks", Raised: RaisedFlat, Color: ColorBrown, Red: RedNone, Pus: PusNone},
			want: GroupPigment,
		},
		{
			name: "flat and skin-coloured is NOT pigment (nothing is discoloured)",
			in:   MorphologyFeatures{Region: "cheeks", Raised: RaisedFlat, Color: ColorSkin, Red: RedNone, Pus: PusNone},
			want: GroupUnknown, wantNeedsInfo: true,
		},
		{
			name: "open pore / black = blackheads",
			in:   MorphologyFeatures{Region: "nose", Raised: RaisedYes, Color: ColorBlack, Red: RedNone, Pus: PusNone, Shape: ShapeOpenPore},
			want: GroupOpenComedones,
		},
		{
			name: "neck creases, no bumps",
			in:   MorphologyFeatures{Region: "neck", Raised: RaisedFlat, Color: ColorSkin, Red: RedNone, Pus: PusNone, Shape: ShapeCrease},
			want: GroupNeckCrease,
		},
		{
			name: "raised skin-colored, shape unreadable = safe read, no user question",
			in:   MorphologyFeatures{Region: "cheeks", Raised: RaisedYes, Color: ColorSkin, Red: RedNone, Pus: PusNone, Shape: ShapeUnknown},
			want: GroupClosedComedones, wantNeedsInfo: true, wantAsksUser: false,
		},
		{
			name: "nothing readable = unknown, never a confident group",
			in:   MorphologyFeatures{Region: "cheeks"},
			want: GroupUnknown, wantNeedsInfo: true, wantAsksUser: false,
		},
	}
}

func TestMorphologyEval_Features(t *testing.T) {
	t.Parallel()
	var failures []string
	for _, c := range featureEvalCases() {
		got := ClassifyMorphology(c.in)
		if got.Group != c.want {
			failures = append(failures, fmt.Sprintf("  %-62s want=%-28s got=%s", c.name, c.want, got.Group))
			continue
		}
		if c.wantNeedsInfo && !got.NeedsMoreInfo {
			failures = append(failures, fmt.Sprintf("  %-62s group ok but should admit it cannot separate look-alikes", c.name))
		}
		if c.wantNeedsInfo && len(got.MissingCues) == 0 {
			failures = append(failures, fmt.Sprintf("  %-62s NeedsMoreInfo without MissingCues", c.name))
		}
		if asks := ShouldAskUser(got); asks != c.wantAsksUser {
			failures = append(failures, fmt.Sprintf("  %-62s ShouldAskUser=%v want %v (alts=%v)", c.name, asks, c.wantAsksUser, got.Alternatives))
		}
	}
	if len(failures) > 0 {
		t.Fatalf("morphology feature eval — %d/%d failed:\n%s",
			len(failures), len(featureEvalCases()), strings.Join(failures, "\n"))
	}
}

type proseCase struct {
	name   string
	prose  string
	region string
	want   MorphologyGroup
}

// proseEvalCases use real-world phrasings the vision passes actually produce.
func proseEvalCases() []proseCase {
	return []proseCase{
		{
			name:   "cheek milia phrasing",
			prose:  "Má của mày đang có nhiều nốt nhỏ màu da nổi cao, trông giống mụn ẩn hoặc milia. Không thấy đỏ sưng hay mủ.",
			region: "cheeks", want: GroupMiliaLike,
		},
		{
			name:   "cheek rough texture phrasing",
			prose:  "Má của mày đang sần sùi rõ, nhiều nốt nhỏ nổi cao + bề mặt da gồ ghề không đều. Không thấy đỏ sưng hay mủ.",
			region: "cheeks", want: GroupRoughTexture,
		},
		{
			name:   "neck skin tag phrasing",
			prose:  "Cổ của mày đang có nhiều nốt nhỏ màu da nổi cao, mềm, có cuống. Trông giống mụn thịt ở cổ.",
			region: "neck", want: GroupSkinTag,
		},
		{
			name:   "inflamed with whiteheads",
			prose:  "Má đang có cụm mụn viêm đỏ sưng, vài nốt đầu trắng rõ.",
			region: "cheeks", want: GroupPustules,
		},
		{
			name:   "neck creases",
			prose:  "Cổ có vài nếp ngang rõ chạy ngang da, không thấy nốt nổi.",
			region: "neck", want: GroupNeckCrease,
		},
		{
			name:   "flat brown pigment",
			prose:  "Má có mảng thâm nâu phẳng, không nổi cục.",
			region: "cheeks", want: GroupPigment,
		},
		{
			name:   "tiny bumps on clearly pink cheeks",
			prose:  "Má mày đang đỏ hồng khá nhiều, kèm nốt nhỏ li ti.",
			region: "cheeks", want: GroupComedonesIrritated,
		},
	}
}

func TestMorphologyEval_Prose(t *testing.T) {
	t.Parallel()
	var failures []string
	for _, c := range proseEvalCases() {
		got := ClassifyMorphology(MorphologyFeaturesFromProse(c.prose, c.region))
		if got.Group != c.want {
			failures = append(failures, fmt.Sprintf("  %-38s want=%-28s got=%-28s prose=%q",
				c.name, c.want, got.Group, c.prose))
		}
	}
	if len(failures) > 0 {
		t.Fatalf("morphology prose eval — %d/%d failed:\n%s",
			len(failures), len(proseEvalCases()), strings.Join(failures, "\n"))
	}
}

func TestProseDeniesRedness_NegationNotConfusedWithCue(t *testing.T) {
	t.Parallel()
	if !ProseDeniesRedness("Không thấy đỏ sưng hay mủ.") {
		t.Fatal("negated redness must be read as no redness (cue words appear inside the negation)")
	}
	if ProseDeniesRedness("Má đang đỏ sưng rõ, không thấy mủ.") {
		t.Fatal("positive redness with a separate pus negation must NOT count as no redness")
	}
}

func TestProseMentionsSkinTagOnFace(t *testing.T) {
	t.Parallel()
	if !ProseMentionsSkinTagOnFace("Má của mày có nhiều nốt màu da, trông giống mụn thịt.") {
		t.Fatal("cheek + mụn thịt must be flagged as a mislabel")
	}
	if ProseMentionsSkinTagOnFace("Cổ của mày trông giống mụn thịt.") {
		t.Fatal("neck skin tags are legitimate and must not be rewritten")
	}
	got := RewriteSkinTagOnFace("Đây trông giống mụn thịt trên má.", "vi")
	if strings.Contains(strings.ToLower(got), "mụn thịt") {
		t.Fatalf("rewrite must remove mụn thịt, got %q", got)
	}
	if !strings.Contains(got, "milia") {
		t.Fatalf("rewrite should name mụn ẩn/milia, got %q", got)
	}
}

func TestAlignOnboardingMorphology_NoRedDropsInflamedLabels(t *testing.T) {
	t.Parallel()
	obs := dto.OnboardingSkinObservations{
		AcneStatus: "inflammatory_acne",
		Redness:    "moderate",
		Texture:    "bumpy",
	}
	out := &dto.OnboardingSkinAnalyzeResponse{
		DetailedObservations: "Má của mày đang có nhiều nốt nhỏ màu da nổi cao, trông giống mụn ẩn hoặc milia. Không thấy đỏ sưng hay mủ.",
		Summary:              "Má có nốt nhỏ màu da, không đỏ sưng.",
		MainConcerns:         []string{"mụn ẩn"},
		ConcernTypes:         []string{"inflammatory_acne", "redness_irritation"},
		Concerns:             []string{"acne", "redness"},
		PrimaryRegions:       []string{"cheeks"},
		SkinObservations:     &obs,
		SeverityLevel:        SeverityDense,
		Phase:                PhaseCalmFirst,
	}
	out.PhotoQuality.Sufficient = true

	if !alignOnboardingMorphology(out, "vi") {
		t.Fatal("expected alignment to change contradictory labels")
	}
	if listHasValue(out.ConcernTypes, "inflammatory_acne") || listHasValue(out.ConcernTypes, "redness_irritation") {
		t.Fatalf("inflamed labels must be dropped when prose says no redness, got %v", out.ConcernTypes)
	}
	if !listHasValue(out.ConcernTypes, "comedones") {
		t.Fatalf("milia/closed comedones must be represented, got %v", out.ConcernTypes)
	}
	if listHasValue(out.Concerns, "redness") {
		t.Fatalf("redness concern must be dropped, got %v", out.Concerns)
	}
	if obs.Redness != "none" {
		t.Fatalf("structured redness cue must be corrected, got %q", obs.Redness)
	}
	if obs.AcneStatus == "inflammatory_acne" {
		t.Fatal("acne_status must not stay inflammatory when prose denies redness")
	}
	if out.SeverityLevel == SeverityDense {
		t.Fatal("severity must be recomputed from corrected cues, not stay dense")
	}
	if out.MorphologyGroup != string(GroupMiliaLike) {
		t.Fatalf("morphology group: got %q want %q", out.MorphologyGroup, GroupMiliaLike)
	}
	// Milia and closed comedones get identical advice, so the user must NOT be
	// interrogated about it — that only makes the read look unsure for no benefit.
	if out.NeedsMoreInfo || len(out.ClarifyQuestions) > 0 {
		t.Fatalf("same-care look-alikes must stay quiet, got clarify=%v", out.ClarifyQuestions)
	}
}

// The one doubt worth surfacing: the photo never says whether the bumps are irritated,
// which flips the advice between calming first and a gentle base.
func TestAlignOnboardingMorphology_AsksOnlyWhenCareWouldChange(t *testing.T) {
	t.Parallel()
	out := &dto.OnboardingSkinAnalyzeResponse{
		DetailedObservations: "Má mày đang có nhiều nốt nhỏ màu da nổi cao, tròn và mịn.",
		PrimaryRegions:       []string{"cheeks"},
	}
	out.PhotoQuality.Sufficient = true
	alignOnboardingMorphology(out, "vi")
	if !out.NeedsMoreInfo || len(out.ClarifyQuestions) == 0 {
		t.Fatal("unreadable redness changes the care direction, so it must ask")
	}

	// Same photo, but the structured cue fills in that there is no redness: no question.
	obs := dto.OnboardingSkinObservations{Redness: "none"}
	quiet := &dto.OnboardingSkinAnalyzeResponse{
		DetailedObservations: "Má mày đang có nhiều nốt nhỏ màu da nổi cao, tròn và mịn.",
		PrimaryRegions:       []string{"cheeks"},
		SkinObservations:     &obs,
	}
	quiet.PhotoQuality.Sufficient = true
	alignOnboardingMorphology(quiet, "vi")
	if quiet.NeedsMoreInfo {
		t.Fatalf("structured redness cue should settle it, got clarify=%v", quiet.ClarifyQuestions)
	}
}

func TestAlignOnboardingMorphology_RewritesFaceSkinTag(t *testing.T) {
	t.Parallel()
	out := &dto.OnboardingSkinAnalyzeResponse{
		DetailedObservations: "Má của mày có nhiều nốt nhỏ màu da nổi cao. Trông giống mụn thịt.",
		MainConcerns:         []string{"mụn thịt"},
		PrimaryRegions:       []string{"cheeks"},
	}
	out.PhotoQuality.Sufficient = true
	alignOnboardingMorphology(out, "vi")
	if strings.Contains(strings.ToLower(out.DetailedObservations), "mụn thịt") {
		t.Fatalf("face prose must not keep mụn thịt: %q", out.DetailedObservations)
	}
	for _, c := range out.MainConcerns {
		if strings.Contains(strings.ToLower(c), "mụn thịt") {
			t.Fatalf("main_concerns must not keep mụn thịt: %v", out.MainConcerns)
		}
	}
}

// A readable photo whose prose simply uses wording the parser does not know must NOT be
// labeled uncertain — otherwise the warning shows up everywhere and stops meaning anything.
func TestAlignOnboardingMorphology_UnparsedProseDoesNotClaimUncertainty(t *testing.T) {
	t.Parallel()
	out := &dto.OnboardingSkinAnalyzeResponse{
		DetailedObservations: "Da mày trông tạm ổn hôm nay, không có gì đáng lo.",
		PrimaryRegions:       []string{"cheeks"},
	}
	out.PhotoQuality.Sufficient = true
	alignOnboardingMorphology(out, "vi")
	if out.NeedsMoreInfo {
		t.Fatalf("unrecognised wording is a parser gap, not photo ambiguity; got clarify=%v", out.ClarifyQuestions)
	}
	if out.MorphologyGroup != "" {
		t.Fatalf("no group should be claimed when nothing was read, got %q", out.MorphologyGroup)
	}
}

func TestAlignOnboardingMorphology_PoorPhotoAsksInstead(t *testing.T) {
	t.Parallel()
	out := &dto.OnboardingSkinAnalyzeResponse{
		DetailedObservations: "Ảnh hơi tối, khó đọc chi tiết.",
		PrimaryRegions:       []string{"cheeks"},
	}
	out.PhotoQuality.Sufficient = false
	alignOnboardingMorphology(out, "vi")
	if !out.NeedsMoreInfo || len(out.ClarifyQuestions) == 0 {
		t.Fatal("an unreadable photo must produce a retake ask, not a confident group")
	}
}

func TestSanitizeCheckInVisionJSON_FixesCheekSkinTag(t *testing.T) {
	t.Parallel()
	raw := `{"photo_assessment":{"lighting":"ok","angle_clarity":"ok","limitations":""},` +
		`"visible_observations":["má: nhiều nốt màu da nổi cao, trông giống mụn thịt"],` +
		`"texture_and_oil_cues":"bề mặt hơi gồ","redness_or_discoloration_cues":"không đỏ","uncertainty_note":""}`
	out, changed := SanitizeCheckInVisionJSON(raw, "vi")
	if !changed {
		t.Fatal("expected cheek skin-tag bullet to be rewritten")
	}
	if strings.Contains(strings.ToLower(out), "mụn thịt") {
		t.Fatalf("sanitized JSON must not keep mụn thịt on cheeks: %s", out)
	}
	if !strings.Contains(out, "milia") {
		t.Fatalf("expected milia wording after rewrite: %s", out)
	}
}

// SanitizeCheckInVisionJSON re-marshals from a struct, so a schema field missing from
// that struct would be dropped before the coach sees it.
func TestSanitizeCheckInVisionJSON_KeepsEverySchemaField(t *testing.T) {
	t.Parallel()
	raw := `{"photo_assessment":{"lighting":"soft window light","angle_clarity":"straight on","limitations":"hơi mờ"},` +
		`"visible_observations":["má: nhiều nốt màu da nổi cao, trông giống mụn thịt","trán: bóng nhẹ"],` +
		`"texture_and_oil_cues":"bề mặt hơi gồ","redness_or_discoloration_cues":"vài đốm thâm nông",` +
		`"uncertainty_note":"góc chụp hẹp"}`
	out, changed := SanitizeCheckInVisionJSON(raw, "vi")
	if !changed {
		t.Fatal("expected the cheek skin-tag bullet to be rewritten")
	}
	for _, must := range []string{
		"soft window light", "straight on", "hơi mờ",
		"trán: bóng nhẹ", "bề mặt hơi gồ", "vài đốm thâm nông", "góc chụp hẹp",
	} {
		if !strings.Contains(out, must) {
			t.Fatalf("sanitizing dropped %q from the vision JSON: %s", must, out)
		}
	}
}

func TestCheckInVisionPhotoLimited(t *testing.T) {
	t.Parallel()
	blurry := `{"photo_assessment":{"limitations":"ảnh hơi mờ, thiếu sáng"},"visible_observations":[]}`
	limited, note := CheckInVisionPhotoLimited(blurry)
	if !limited || note == "" {
		t.Fatalf("blurry photo must be flagged, got limited=%v note=%q", limited, note)
	}
	clean := `{"photo_assessment":{"limitations":""},"visible_observations":["má: vài nốt đỏ"],"uncertainty_note":""}`
	if limited, _ := CheckInVisionPhotoLimited(clean); limited {
		t.Fatal("clean photo must not be flagged as limited")
	}
	if len(RetakePhotoTips("vi")) < 2 {
		t.Fatal("retake tips must include the oblique-light ask")
	}
	if !strings.Contains(strings.Join(RetakePhotoTips("vi"), " "), "ngang") {
		t.Fatal("VI retake tips must mention side/oblique lighting")
	}
}

func TestGroupMappingsCoverEveryGroup(t *testing.T) {
	t.Parallel()
	groups := []MorphologyGroup{
		GroupInflamedAcne, GroupPustules, GroupClosedComedones, GroupComedonesIrritated,
		GroupMiliaLike, GroupRoughTexture, GroupOpenComedones, GroupPigment,
		GroupSkinTag, GroupNeckCrease,
	}
	for _, g := range groups {
		if MorphologyGroupLabel(g, "vi") == "" || MorphologyGroupLabel(g, "en") == "" {
			t.Fatalf("group %q missing a user-facing label", g)
		}
		if AdminConcernForGroup(g) == "" {
			t.Fatalf("group %q missing an admin concern mapping", g)
		}
	}
	// Care-advice gate: only these three mean "calm first, no acids".
	for _, g := range []MorphologyGroup{GroupInflamedAcne, GroupPustules, GroupComedonesIrritated} {
		if !GroupImpliesRedness(g) {
			t.Fatalf("group %q must imply redness for care gating", g)
		}
	}
	for _, g := range []MorphologyGroup{GroupMiliaLike, GroupRoughTexture, GroupSkinTag, GroupNeckCrease, GroupPigment} {
		if GroupImpliesRedness(g) {
			t.Fatalf("group %q must NOT trigger calm-first/acid warnings", g)
		}
	}
}

// The exporter turns operator corrections into labels, so this mapping decides what the
// eval set actually measures. Wrong here = measuring the wrong thing.
func TestGroupFromCorrectedArea(t *testing.T) {
	t.Parallel()
	cases := []struct {
		concern, region, note string
		want                  MorphologyGroup
		wantExact             bool
	}{
		{"texture", "cheeks", "Má sần sùi rõ, bề mặt gồ ghề không đều.", GroupRoughTexture, true},
		{"texture", "neck", "Cổ có vài nếp ngang rõ.", GroupNeckCrease, true},
		{"other", "neck", "Nốt màu da nổi cao, trông giống mụn thịt.", GroupSkinTag, true},
		{"acne", "cheeks", "Nốt nhỏ màu da, trông giống mụn ẩn hoặc milia.", GroupMiliaLike, true},
		{"acne", "nose", "Lỗ đen nhỏ, đầu đen rõ.", GroupOpenComedones, true},
		{"acne", "cheeks", "Nhiều nốt nhỏ li ti dưới da.", GroupClosedComedones, true},
		{"papules", "chin", "Nốt đỏ sưng rải rác.", GroupInflamedAcne, true},
		{"pustules", "cheeks", "Có đầu trắng rõ.", GroupPustules, true},
		{"pigmentation", "chin", "Thâm nâu quanh mép.", GroupPigment, true},
		// Enums that legitimately cover several groups must NOT be turned into a label.
		{"redness", "cheeks", "Má ửng đỏ.", GroupUnknown, false},
		{"pores", "nose", "Lỗ chân lông to.", GroupUnknown, false},
		{"other", "cheeks", "Nốt nhỏ trên má.", GroupUnknown, false},
	}
	for _, c := range cases {
		got, exact := GroupFromCorrectedArea(c.concern, c.region, c.note)
		if exact != c.wantExact {
			t.Fatalf("concern=%q region=%q: exact=%v want %v (got group %q)", c.concern, c.region, exact, c.wantExact, got)
		}
		if exact && got != c.want {
			t.Fatalf("concern=%q region=%q: got %q want %q", c.concern, c.region, got, c.want)
		}
	}
}

func TestWorstConfidence(t *testing.T) {
	t.Parallel()
	if got := WorstConfidence(ConfidenceHigh, ConfidenceLow, ConfidenceMedium); got != ConfidenceLow {
		t.Fatalf("got %q want low", got)
	}
	if got := WorstConfidence(ConfidenceHigh, ConfidenceHigh); got != ConfidenceHigh {
		t.Fatalf("got %q want high", got)
	}
}
