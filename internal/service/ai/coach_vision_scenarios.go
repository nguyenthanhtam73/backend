package ai

import (
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// VisionCoachScenario is a check-in QA fixture with realistic VISION_SUMMARY_JSON
// representing common Vietnamese skin concerns (oily/acne, dry, hyperpigmentation, combo).
type VisionCoachScenario struct {
	ID          string
	Label       string
	Persona     CoachPersona
	VisionJSON  string
	WantRegions []string
	WantCues    []string
}

// VisionCoachScenarios returns 6 photo-backed coach scenarios for v16 QA.
func VisionCoachScenarios() []VisionCoachScenario {
	return []VisionCoachScenario{
		scenarioOilyAcneVision(),
		scenarioDrySkinVision(),
		scenarioHyperpigmentationVision(),
		scenarioComboBarrierVision(),
		scenarioSevereAcneVision(),
		scenarioMelasmaVision(),
	}
}

func (s VisionCoachScenario) CoachUserMessage() string {
	p := s.Persona
	fullCtx := p.FullContextWithMemory()
	var b strings.Builder
	b.WriteString("The following VISION_SUMMARY_JSON was produced by a separate vision-only pass over the user's check-in photos. It is NOT a diagnosis — only soft visual cues.\n\n")
	b.WriteString("VISION_SUMMARY_JSON:\n")
	b.WriteString(s.VisionJSON)
	if priority := prependCoachActionPriority(fullCtx); priority != "" {
		b.WriteString("\n\n")
		b.WriteString(priority)
	}
	b.WriteString("\n\nUSER_CONTEXT (saved profile + today's self-report + environment):\n")
	b.WriteString(fullCtx)
	b.WriteString(coachTurnChecklist(fullCtx, true))
	b.WriteString("\n\nNow produce the FINAL coach output as ONE JSON object matching this schema exactly.\n\n")
	b.WriteString(CoachOutputJSONSchemaBlock)
	return b.String()
}

func scenarioOilyAcneVision() VisionCoachScenario {
	p := personaBeginnerOily()
	p.TodayCheck.UserNote = "T-zone bóng dầu buổi chiều, cằm nổi thêm vài mụn đỏ. Ngồi máy lạnh cả ngày."
	vision := `{
  "photo_assessment": {"lighting": "indoor fluorescent, even", "angle_clarity": "front face, T-zone visible", "limitations": "single angle, no UV fluorescence"},
  "visible_observations": [
    "T-zone (forehead and nose) shows noticeable oil sheen under indoor light",
    "4-5 small inflamed red papules clustered on chin and jawline",
    "enlarged pores visible on nose and inner cheek",
    "cheeks appear slightly matte compared to T-zone"
  ],
  "texture_and_oil_cues": "oily sheen on T-zone, mixed matte cheeks — consistent with combo-oily pattern",
  "redness_or_discoloration_cues": "localized red bumps on chin; no widespread facial redness",
  "uncertainty_note": "photo cannot confirm depth of inflammation or product irritation"
}`
	return VisionCoachScenario{
		ID:         "oily_acne",
		Label:      "Da dầu mụn viêm (T-zone + cằm)",
		Persona:    p,
		VisionJSON: vision,
		WantRegions: []string{"t-zone", "trán", "mũi", "cằm"},
		WantCues:    []string{"dầu", "bóng", "mụn", "đỏ", "lỗ chân lông"},
	}
}

func scenarioDrySkinVision() VisionCoachScenario {
	p := personaStrongAdherence()
	p.SkillLevel = "beginner"
	p.TodayCheck.UserNote = "Má căng khô, có vảy nhẹ khi soi gương. Tick đủ routine 4 ngày."
	if p.Profile != nil {
		p.Profile.SkillLevel = domain.SkillLevelBeginner
	}
	vision := `{
  "photo_assessment": {"lighting": "soft window light", "angle_clarity": "cheeks and forehead visible", "limitations": "fine flaking may be subtle on camera"},
  "visible_observations": [
    "cheeks look matte with fine dry flaking near nasolabial folds",
    "skin appears dull/low glow on cheek apples",
    "no obvious active inflamed acne lesions",
    "forehead relatively smooth but slightly tight-looking texture"
  ],
  "texture_and_oil_cues": "low surface oil, dry matte finish on cheeks — barrier may feel tight",
  "redness_or_discoloration_cues": "mild pink tone on cheek apples, no deep erythema",
  "uncertainty_note": "dehydration vs true dryness cannot be distinguished from photo alone"
}`
	return VisionCoachScenario{
		ID:         "dry_skin",
		Label:      "Da khô / barrier yếu",
		Persona:    p,
		VisionJSON: vision,
		WantRegions: []string{"má", "cheek", "trán"},
		WantCues:    []string{"khô", "căng", "flak", "xỉn", "dull"},
	}
}

func scenarioHyperpigmentationVision() VisionCoachScenario {
	p := personaIntermediateCombo()
	p.TodayCheck.UserNote = "Thâm sau mụn ở má phải vẫn còn, T-zone hơi dầu. Ngủ muộn tuần này."
	vision := `{
  "photo_assessment": {"lighting": "daylight selfie", "angle_clarity": "both cheeks visible", "limitations": "melasma vs PIH cannot be confirmed visually"},
  "visible_observations": [
    "multiple flat brown post-inflammatory marks on right cheek",
    "lighter brown spots scattered on left cheek",
    "T-zone mild oil sheen on nose",
    "overall tone slightly uneven between cheek and forehead"
  ],
  "texture_and_oil_cues": "T-zone slightly shiny; cheek texture otherwise smooth",
  "redness_or_discoloration_cues": "brown macules (post-acne marks) on cheeks — no active red cysts",
  "uncertainty_note": "cannot determine depth of pigmentation or need for prescription agents"
}`
	return VisionCoachScenario{
		ID:         "hyperpigmentation",
		Label:      "Da thâm sau mụn",
		Persona:    p,
		VisionJSON: vision,
		WantRegions: []string{"má", "cheek", "mũi", "trán"},
		WantCues:    []string{"thâm", "brown", "đốm", "dầu", "tone"},
	}
}

func scenarioComboBarrierVision() VisionCoachScenario {
	p := personaFrequentNotHelpful()
	p.TodayCheck.UserNote = "Má đỏ và châm chích, trán hơi dầu. Không dám thử sản phẩm mới."
	vision := `{
  "photo_assessment": {"lighting": "bathroom warm light", "angle_clarity": "cheeks and T-zone visible", "limitations": "redness intensity varies with lighting temperature"},
  "visible_observations": [
    "both cheeks show diffuse pink-red tone",
    "forehead and nose with mild oil sheen",
    "no large pus-filled lesions; a few tiny closed bumps on chin",
    "skin surface looks slightly rough on cheek apples"
  ],
  "texture_and_oil_cues": "combo pattern — oily T-zone with irritated-looking cheeks",
  "redness_or_discoloration_cues": "facial redness concentrated on cheeks; barrier stress likely",
  "uncertainty_note": "cannot distinguish rosacea, irritation, or temporary flush"
}`
	return VisionCoachScenario{
		ID:         "combo_barrier",
		Label:      "Da combo + barrier yếu / nhạy cảm",
		Persona:    p,
		VisionJSON: vision,
		WantRegions: []string{"má", "trán", "mũi", "cằm"},
		WantCues:    []string{"đỏ", "dầu", "châm", "dịu", "barrier"},
	}
}

func scenarioSevereAcneVision() VisionCoachScenario {
	p := personaBeginnerOily()
	p.TodayCheck.UserNote = "Mụn viêm nhiều vùng cằm và hàm, T-zone bóng dầu. Stress tuần này, ngủ muộn."
	vision := `{
  "photo_assessment": {"lighting": "daylight near window", "angle_clarity": "chin and jawline clearly visible", "limitations": "depth of cysts cannot be confirmed"},
  "visible_observations": [
    "8-12 inflamed red papules and pustules clustered on chin and jawline",
    "T-zone forehead and nose with moderate to strong oil sheen",
    "enlarged pores on nose and adjacent cheeks",
    "post-inflammatory brown marks scattered on lower cheeks"
  ],
  "texture_and_oil_cues": "oily T-zone with active inflammatory lesions on lower face",
  "redness_or_discoloration_cues": "multiple red inflamed bumps on chin/jaw; brown marks on cheeks",
  "uncertainty_note": "cannot distinguish acne severity grade or need for prescription care from photo alone"
}`
	return VisionCoachScenario{
		ID:          "severe_acne",
		Label:       "Da mụn nhiều (viêm + thâm)",
		Persona:     p,
		VisionJSON:  vision,
		WantRegions: []string{"cằm", "hàm", "trán", "mũi", "má"},
		WantCues:    []string{"mụn", "viêm", "đỏ", "dầu", "thâm", "lỗ chân lông"},
	}
}

func scenarioMelasmaVision() VisionCoachScenario {
	p := personaIntermediateCombo()
	p.TodayCheck.UserNote = "Thâm nám má hai bên rõ hơn, đi nắng cuối tuần qua. T-zone vẫn hơi dầu."
	vision := `{
  "photo_assessment": {"lighting": "outdoor indirect sunlight", "angle_clarity": "both cheeks and forehead visible", "limitations": "melasma vs sun spots vs PIH cannot be confirmed clinically"},
  "visible_observations": [
    "symmetric brown patches on both malar cheeks (cheekbone area)",
    "additional lighter brown macules on forehead temples",
    "T-zone mild oil sheen on nose",
    "no obvious active red inflamed acne today"
  ],
  "texture_and_oil_cues": "cheeks matte with pigmentation patches; nose slightly shiny",
  "redness_or_discoloration_cues": "brown melasma-like patches on cheeks; uneven tone between cheek and forehead",
  "uncertainty_note": "photo alone cannot confirm melasma diagnosis or depth of pigmentation"
}`
	return VisionCoachScenario{
		ID:          "melasma",
		Label:       "Da thâm nám (má hai bên)",
		Persona:     p,
		VisionJSON:  vision,
		WantRegions: []string{"má", "cheek", "trán", "mũi"},
		WantCues:    []string{"nám", "thâm", "brown", "patch", "dầu", "tone"},
	}
}
