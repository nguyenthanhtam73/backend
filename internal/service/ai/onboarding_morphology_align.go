package ai

import (
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

// onboarding_morphology_align.go — the onboarding/user-side counterpart of
// AlignAdminSkinAnalysisWithQuestion. Before this existed, a vision mislabel went
// straight to the user: milia read as "mụn thịt", smooth bumps labeled
// inflammatory_acne, calm_first triggered by bumpiness alone. Each of those pushes
// the starter routine the wrong way, so labels are reconciled with the prose here,
// while the response is still being built (before product guidance is derived).

// alignOnboardingMorphology reconciles concern labels + structured cues with what the
// vision prose actually says, and records the morphology verdict (group, confidence,
// clarify questions) on the response. Returns true when anything changed.
func alignOnboardingMorphology(out *dto.OnboardingSkinAnalyzeResponse, locale string) bool {
	if out == nil {
		return false
	}
	changed := false

	// 1. Face bumps must never be called mụn thịt / skin tags.
	if ProseMentionsSkinTagOnFace(out.DetailedObservations) {
		out.DetailedObservations = RewriteSkinTagOnFace(out.DetailedObservations, locale)
		changed = true
	}
	if ProseMentionsSkinTagOnFace(out.Summary) {
		out.Summary = RewriteSkinTagOnFace(out.Summary, locale)
		changed = true
	}
	for i, c := range out.MainConcerns {
		if ProseMentionsSkinTagOnFace(c) || proseHasPositiveCue(c, cueSkinTag...) {
			out.MainConcerns[i] = MorphologyGroupLabel(GroupMiliaLike, locale)
			changed = true
		}
	}

	region := "cheeks"
	if len(out.PrimaryRegions) > 0 && strings.TrimSpace(out.PrimaryRegions[0]) != "" {
		region = out.PrimaryRegions[0]
	}
	prose := strings.Join([]string{
		out.DetailedObservations,
		out.Summary,
		strings.Join(out.MainConcerns, ". "),
	}, ". ")

	verdict := ClassifyMorphology(MorphologyFeaturesFromProse(prose, region))

	// 2. Prose says no redness → strip inflamed/irritation labels and fix the cues that
	// drive severity/phase, otherwise the user gets calm-first advice for calm skin
	// (or "nốt đỏ sưng" chips over a photo with no red at all).
	if ProseDeniesRedness(prose) && !GroupImpliesRedness(verdict.Group) {
		if next, dropped := dropConcernTypes(out.ConcernTypes, "inflammatory_acne", "redness_irritation"); dropped {
			out.ConcernTypes = next
			changed = true
		}
		if next, dropped := dropConcernTypes(out.Concerns, "redness"); dropped {
			out.Concerns = next
			changed = true
		}
		if out.SkinObservations != nil {
			obs := out.SkinObservations
			if obsRednessLevels.has(obs.Redness) {
				obs.Redness = "none"
				changed = true
			}
			switch normLower(obs.AcneStatus) {
			case "inflammatory_acne", "cystic_acne":
				obs.AcneStatus = "few_whiteheads"
				changed = true
			}
			// Severity/phase were derived from the wrong cues — recompute from corrected ones.
			out.SeverityLevel = deriveSeverityFromObservations(*obs)
			out.Phase = derivePhase(out.SeverityLevel, *obs)
			out.BarrierSignal = inferOnboardingBarrierSignal(obs.Redness, obs.Texture)
		}
	}

	// 3. The classified group must be represented in concern_types.
	wantTypes := []string{OnboardingConcernTypeForGroup(verdict.Group)}
	if verdict.Group == GroupComedonesIrritated {
		// Tiny bumps on a pink base are BOTH things — dropping the irritation half is how
		// advice ends up pushing acids onto skin that needs calming first.
		wantTypes = append(wantTypes, "redness_irritation")
	}
	for _, ct := range wantTypes {
		if ct == "" {
			continue
		}
		if _, ok := allowedConcernTypes[ct]; ok && !listHasValue(out.ConcernTypes, ct) {
			out.ConcernTypes = append(out.ConcernTypes, ct)
			changed = true
		}
	}

	// 4. Record the verdict so the UI can show honest uncertainty instead of a guess.
	//
	// GroupUnknown here usually means the prose simply used wording the feature parser
	// does not recognise — a parser gap, NOT an unreadable photo. Flagging those would
	// put "chưa đủ chốt" on perfectly clear photos and train users to ignore the warning,
	// so only a real look-alike ambiguity raises it.
	if verdict.Group != GroupUnknown {
		out.MorphologyGroup = string(verdict.Group)
		out.GroupConfidence = verdict.Confidence
		out.NeedsMoreInfo = verdict.NeedsMoreInfo
		out.ClarifyQuestions = MorphologyClarifyQuestions(verdict, locale)
	}
	if !out.PhotoQuality.Sufficient {
		// A photo we cannot read is exactly when to ask rather than assert.
		out.NeedsMoreInfo = true
		if len(out.ClarifyQuestions) == 0 {
			out.ClarifyQuestions = RetakePhotoTips(locale)
		}
	}
	return changed
}

func listHasValue(list []string, want string) bool {
	want = normLower(want)
	for _, v := range list {
		if normLower(v) == want {
			return true
		}
	}
	return false
}

func dropConcernTypes(list []string, drop ...string) ([]string, bool) {
	if len(list) == 0 {
		return list, false
	}
	remove := make(map[string]struct{}, len(drop))
	for _, d := range drop {
		remove[normLower(d)] = struct{}{}
	}
	out := make([]string, 0, len(list))
	dropped := false
	for _, v := range list {
		if _, bad := remove[normLower(v)]; bad {
			dropped = true
			continue
		}
		out = append(out, v)
	}
	if !dropped {
		return list, false
	}
	return out, true
}
