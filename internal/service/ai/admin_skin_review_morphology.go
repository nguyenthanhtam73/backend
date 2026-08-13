package ai

import (
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

// admin_skin_review_morphology.go — runs the shared Go classifier over an admin
// analysis: records the morphology group + honest confidence, fixes concern enums the
// prose contradicts, and downgrades confidence when two independent vision samples
// disagree (that disagreement is the signal the photo cannot be read, not noise).

// adminSkinReviewVisionSamples is how many independent vision samples to draw per
// analysis. The extra sample runs in parallel (no added latency) and is used only to
// detect disagreement: same photo, two reads, different group → say "chưa đủ chốt"
// instead of shipping a coin flip into care advice. Set to 1 to disable.
const adminSkinReviewVisionSamples = 2

// adminPrimaryProblemArea returns the attention area the analysis is really about.
func adminPrimaryProblemArea(a *dto.AdminSkinReviewAnalysis) (dto.AdminSkinAttentionArea, bool) {
	if a == nil {
		return dto.AdminSkinAttentionArea{}, false
	}
	var first dto.AdminSkinAttentionArea
	found := false
	for _, ar := range a.AttentionAreas {
		c := normLower(ar.Concern)
		if c == "" || c == "none" || c == "not_visible" {
			continue
		}
		if !found {
			first, found = ar, true
		}
		// Cheeks carry the look-alike cases we care most about getting right.
		if normLower(ar.Region) == "cheeks" {
			return ar, true
		}
	}
	return first, found
}

// adminMorphologyVerdict classifies the analysis from its own prose.
func adminMorphologyVerdict(a *dto.AdminSkinReviewAnalysis) MorphologyVerdict {
	if a == nil {
		return MorphologyVerdict{Group: GroupUnknown, Confidence: ConfidenceLow, NeedsMoreInfo: true}
	}
	area, ok := adminPrimaryProblemArea(a)
	region := "cheeks"
	prose := a.Overview
	if ok {
		region = area.Region
		// Region note is the most specific description of the actual finding.
		if strings.TrimSpace(area.Note) != "" {
			prose = area.Note + ". " + a.Overview
		}
	}
	return ClassifyMorphology(MorphologyFeaturesFromProse(prose, region))
}

// applyAdminMorphologyVerdict records the verdict and repairs concern enums that the
// prose contradicts (e.g. a "papules" chip over a note saying "không thấy đỏ sưng").
// Returns true when a field changed.
func applyAdminMorphologyVerdict(a *dto.AdminSkinReviewAnalysis, locale string) bool {
	if a == nil {
		return false
	}
	verdict := adminMorphologyVerdict(a)
	changed := false

	// Per-area enum repair: a note that denies redness must not keep a red-bump concern.
	for i := range a.AttentionAreas {
		ar := &a.AttentionAreas[i]
		c := normLower(ar.Concern)
		if c == "" || c == "none" || c == "not_visible" {
			continue
		}
		if !ProseDeniesRedness(ar.Note) {
			continue
		}
		areaVerdict := ClassifyMorphology(MorphologyFeaturesFromProse(ar.Note, ar.Region))
		if GroupImpliesRedness(areaVerdict.Group) {
			continue
		}
		if want := AdminConcernForGroup(areaVerdict.Group); want != "" && want != c {
			switch c {
			case "papules", "pustules", "redness", "irritation":
				ar.Concern = want
				changed = true
			}
		}
	}

	// GroupUnknown almost always means the prose used wording the feature parser does not
	// recognise (a parser gap), not an unreadable photo — flagging those would put
	// "chưa đủ chốt" on clear reviews and make the warning meaningless. Sample
	// disagreement, checked separately, is the signal that IS about the photo.
	if verdict.Group == GroupUnknown {
		return changed
	}
	a.MorphologyGroup = string(verdict.Group)
	a.Confidence = verdict.Confidence
	// Same rule as the user flow: only raise "chưa đủ chốt" when the answer would change
	// the care direction. This analysis is also published on the public share page, so a
	// banner over look-alikes that share the same advice would be noise there too.
	if ShouldAskUser(verdict) {
		a.NeedsMoreInfo = true
		a.ClarifyQuestions = MorphologyClarifyQuestions(verdict, locale)
	}
	return changed
}

// downgradeAdminConfidenceOnDisagreement compares the primary analysis with an
// independent second sample of the same photos. Different group = the photo does not
// carry enough signal, so the review says so instead of picking one at random.
func downgradeAdminConfidenceOnDisagreement(primary, second *dto.AdminSkinReviewAnalysis, locale string) bool {
	if primary == nil || second == nil {
		return false
	}
	a := adminMorphologyVerdict(primary)
	b := adminMorphologyVerdict(second)
	if a.Group == b.Group {
		return false
	}
	// Two reads of one photo disagreeing always means the photo is hard, so confidence
	// drops. But only escalate to "chưa đủ chốt" when the two groups would lead to
	// different care — disagreeing between milia and closed comedones changes nothing.
	primary.Confidence = ConfidenceLow
	if CareDirectionForGroup(a.Group) == CareDirectionForGroup(b.Group) {
		return true
	}
	primary.NeedsMoreInfo = true
	extra := MorphologyClarifyQuestions(MorphologyVerdict{
		NeedsMoreInfo: true,
		MissingCues:   []string{CueObliqueLight, CuePain, CueDuration},
	}, locale)
	primary.ClarifyQuestions = mergeClarifyQuestions(primary.ClarifyQuestions, extra)
	return true
}

func mergeClarifyQuestions(existing, extra []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(extra))
	out := make([]string, 0, len(existing)+len(extra))
	for _, list := range [][]string{existing, extra} {
		for _, q := range list {
			q = strings.TrimSpace(q)
			if q == "" {
				continue
			}
			key := strings.ToLower(q)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, q)
		}
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

// AdminSkinContextBlock renders the operator's touch/pain/duration answers for the
// vision prompt. These separate look-alikes a photo physically cannot (milia vs
// closed comedones vs skin tags; acute lip-edge vs old pigment).
func AdminSkinContextBlock(skinContext, locale string) string {
	s := strings.TrimSpace(skinContext)
	if s == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		return "\n\nWHAT THE USER REPORTS ABOUT TOUCH / TIMING (hard evidence — outranks guessing from pixels):\n" + s +
			"\nFirm grain-like + unchanged for months → milia. Soft/stalked on neck/axilla → skin tags. Comes and goes → closed comedones. Painful + appeared fast → acute inflammation. Use this to pick the group; do NOT contradict it."
	}
	return "\n\nUSER KỂ VỀ SỜ / THỜI GIAN (bằng chứng cứng — hơn việc đoán từ pixel):\n" + s +
		"\nCứng như hạt + nhiều tháng không đổi → milia. Mềm / có cuống ở cổ–nách → mụn thịt. Lên xuống theo đợt → mụn ẩn. Đau + nổi nhanh → viêm cấp. Dùng cái này để chọn nhóm; CẤM kết luận trái với nó."
}
