package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

// wardrobeInsightFacts is the skin context actually placed in the cabinet prompt.
// SkinKnown matches the older "do not invent a fit" switch (any usable profile
// field, or a recent check-in). SkinTypeKnown is stricter: the onboarding skin
// type itself is on file, so the model must not claim the skin is unknown.
type wardrobeInsightFacts struct {
	Prompt        string
	SkinKnown     bool
	SkinTypeKnown bool
	SkinType      string
	Concerns      []string
	Goal          string
	Experience    string
	Sensitivity   string
	Anchors       []string
	AcneProne     bool
	Irritated     bool
	HeavyForFace  bool
}

func assembleWardrobeInsightFacts(req WardrobeProductInsightRequest) wardrobeInsightFacts {
	var facts wardrobeInsightFacts
	skinRaw, goalRaw, skillRaw, notes, sensitivity := "", "", "", "", ""
	var concernRaws []string
	if p := req.Profile; p != nil {
		skinRaw = strings.TrimSpace(p.SkinType)
		if p.SkillLevel != "" && p.SkillLevel != domain.SkillLevelUnspecified {
			skillRaw = string(p.SkillLevel)
		}
		notes = strings.TrimSpace(p.Notes)
		concernRaws, _ = dto.DecodeStringSlice(p.Concerns)
		if snap := snapshotMap(p.OnboardingSnapshot); snap != nil {
			if goalRaw == "" {
				goalRaw = snapString(snap, "goal")
			}
			if skinRaw == "" {
				skinRaw = snapString(snap, "skin_type")
			}
			if skillRaw == "" {
				skillRaw = snapString(snap, "skill_level")
			}
			concernRaws = append(concernRaws, snapStrings(snap, "body_concerns")...)
			if s := snapString(snap, "sensitivity"); s != "" {
				sensitivity = s
			}
		}
	}
	facts.SkinType = wardrobeSkinTypeLabel(skinRaw)
	facts.Goal = wardrobeGoalLabel(goalRaw)
	facts.Experience = wardrobeExperienceLabel(skillRaw)
	facts.Concerns = wardrobeConcernLabels(concernRaws, skinRaw, goalRaw)
	if sensitivity == "" {
		sensitivity = wardrobeSensitivityLabel(skinRaw, notes, concernRaws)
	} else if mapped := wardrobeSensitivityPhrase(sensitivity); mapped != "" {
		sensitivity = mapped
	}
	if sensitivity == "" && req.Profile != nil && barrierCompromised(req.Profile.OnboardingSnapshot) {
		sensitivity = "Da dễ kích ứng hơn bình thường"
	}
	facts.Sensitivity = sensitivity
	facts.SkinTypeKnown = facts.SkinType != ""
	facts.Anchors = wardrobeInsightAnchors(facts.SkinType, facts.Goal, facts.Concerns)
	facts.AcneProne = wardrobeAcneProne(facts.SkinType, facts.Goal, notes, strings.Join(facts.Concerns, " "), strings.Join(concernRaws, " "), goalRaw)
	facts.Irritated = recentShowsIrritation(limitRecentForInsight(req.Recent))
	facts.HeavyForFace = productTooHeavyForAcneFace(req.Name, req.Brand, req.Category, req.Notes)

	skinBlock := BuildSkinProfileContext(req.Profile)
	recentBlock := BuildRecentCheckInsContext(limitRecentForInsight(req.Recent))
	facts.SkinKnown = wardrobeInsightSkinKnown(skinBlock, recentBlock)
	facts.Prompt = renderWardrobeInsightPrompt(req, facts, notes, recentBlock)
	return facts
}

func renderWardrobeInsightPrompt(req WardrobeProductInsightRequest, facts wardrobeInsightFacts, notes, recentBlock string) string {
	var b strings.Builder
	b.WriteString("OWNED: this product is already in the user's cabinet. Judge keep-using (nên dùng tiếp) versus not yet (chưa nên dùng tiếp) for the skin type, concerns, and goal below. Do not recommend buying.\n\n")
	b.WriteString("PRODUCT (label text only):\n")
	fmt.Fprintf(&b, "- name: %s\n", oneLineField(req.Name, 200))
	fmt.Fprintf(&b, "- brand: %s\n", oneLineField(req.Brand, 120))
	fmt.Fprintf(&b, "- category: %s\n", oneLineField(req.Category, 64))
	fmt.Fprintf(&b, "- notes: %s\n", oneLineField(req.Notes, 400))
	b.WriteString("\nSKIN_PROFILE (saved onboarding answers — use these fields):\n")
	fmt.Fprintf(&b, "- skin type: %s\n", knownOrUnknown(facts.SkinType))
	fmt.Fprintf(&b, "- concerns: %s\n", knownOrUnknown(joinVietnamese(facts.Concerns)))
	fmt.Fprintf(&b, "- goal: %s\n", knownOrUnknown(facts.Goal))
	fmt.Fprintf(&b, "- experience: %s\n", knownOrUnknown(facts.Experience))
	if facts.Sensitivity != "" {
		fmt.Fprintf(&b, "- sensitivity: %s\n", facts.Sensitivity)
	}
	if notes != "" {
		fmt.Fprintf(&b, "- notes: %s\n", oneLineField(notes, 400))
	}
	if facts.SkinTypeKnown {
		fmt.Fprintf(&b, "\nSKIN_PROFILE_STATUS: skin type is known (%s). ", facts.SkinType)
		b.WriteString("Judge fit from this profile. A missing check-in is not missing skin information. ")
		b.WriteString("Do not write \"chưa có thông tin đầy đủ\", \"không có thông tin\", or \"chưa đủ thông tin\".\n")
	} else {
		b.WriteString("\nSKIN_PROFILE_STATUS: skin type is not on file. verdict must be maybe. buy.advice must be \"chưa nên\". Do not invent a skin type.\n")
	}
	if strings.TrimSpace(recentBlock) != "" {
		b.WriteString("\n")
		b.WriteString(recentBlock)
	} else {
		b.WriteString("\nRECENT_CHECK_INS: none. There is no recent irritation note. Do not invent irritation, and do not treat the empty check-in list as unknown skin.\n")
	}
	if facts.HeavyForFace && facts.AcneProne {
		b.WriteString("\nPRODUCT_FIT_HINT: this looks like a body product or a heavy oil/occlusive, and this person is acne-prone. fit.verdict must be no. buy.advice must be \"chưa nên\". The reason must say it is too heavy or too occlusive for this person's skin type, concerns, or goal.\n")
	}
	return b.String()
}

func knownOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return strings.TrimSpace(s)
}

func wardrobeSkinTypeLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	switch normLower(raw) {
	case "prefer_not", "unknown", "unsure", "chưa rõ", "chua ro":
		return ""
	}
	if s, ok := friendlySkinTypeVI[normLower(raw)]; ok {
		return s
	}
	return strings.ToLower(raw)
}

func wardrobeGoalLabel(raw string) string {
	switch normLower(raw) {
	case "", "unsure", "unknown", "prefer_not":
		return ""
	case "clear_acne":
		return "Giảm mụn"
	case "glow":
		return "Da sáng khỏe"
	case "barrier":
		return "Da bớt kích ứng"
	case "anti_aging":
		return "Da săn hơn theo thời gian"
	default:
		return strings.TrimSpace(raw)
	}
}

func wardrobeExperienceLabel(raw string) string {
	switch normLower(raw) {
	case "", "unspecified":
		return ""
	case "beginner":
		return "Mới bắt đầu"
	case "intermediate":
		return "Đã chăm một thời gian"
	case "advanced":
		return "Đã quen chăm da"
	default:
		return strings.TrimSpace(raw)
	}
}

func wardrobeConcernLabels(raws []string, skinRaw, goalRaw string) []string {
	seen := map[string]struct{}{}
	var out []string
	skinLabel := wardrobeSkinTypeLabel(skinRaw)
	goalLabel := wardrobeGoalLabel(goalRaw)
	for _, raw := range raws {
		if wardrobeConcernIsNoise(raw, skinRaw, goalRaw, skinLabel, goalLabel) {
			continue
		}
		label := strings.ToLower(strings.TrimSpace(friendlyConcern(raw, "vi")))
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}

func wardrobeConcernIsNoise(raw, skinRaw, goalRaw, skinLabel, goalLabel string) bool {
	key := normLower(raw)
	if key == "" {
		return true
	}
	if key == normLower(skinRaw) || key == normLower(goalRaw) {
		return true
	}
	if skinLabel != "" && key == normLower(skinLabel) {
		return true
	}
	if goalLabel != "" && key == normLower(goalLabel) {
		return true
	}
	switch key {
	case "clear_acne", "glow", "barrier", "anti_aging", "unsure",
		"dry", "oily", "combo", "combination", "normal", "sensitive", "prefer_not",
		"beginner", "intermediate", "advanced", "unspecified":
		return true
	default:
		return false
	}
}

func wardrobeSensitivityLabel(skinRaw, notes string, concerns []string) string {
	blob := strings.ToLower(skinRaw + " " + notes + " " + strings.Join(concerns, " "))
	return wardrobeSensitivityPhrase(blob)
}

func wardrobeSensitivityPhrase(blob string) string {
	blob = strings.ToLower(blob)
	switch {
	case strings.Contains(blob, "sensitive"), strings.Contains(blob, "nhạy"), strings.Contains(blob, "nhay"):
		return "Da nhạy cảm"
	case strings.Contains(blob, "weak_barrier"), strings.Contains(blob, "redness"), strings.Contains(blob, "kích ứng"):
		return "Da dễ kích ứng"
	default:
		return ""
	}
}

func wardrobeAcneProne(parts ...string) bool {
	blob := strings.ToLower(strings.Join(parts, " "))
	for _, cue := range []string{"mụn", "mun", "acne", "clear_acne", "giảm mụn", "giam mun"} {
		if strings.Contains(blob, cue) {
			return true
		}
	}
	return false
}

func recentShowsIrritation(recent []domain.SkinCheck) bool {
	cues := []string{"rát", "kích ứng", "châm chích", "bong tróc", "nóng rát", "stinging", "irritated"}
	for _, c := range recent {
		syms, _ := dto.DecodeStringSlice(c.Symptoms)
		conds, _ := dto.DecodeStringSlice(c.Conditions)
		blob := strings.ToLower(strings.Join([]string{
			c.UserNote, c.Title, c.EnvironmentNote,
			strings.Join(syms, " "), strings.Join(conds, " "),
		}, " "))
		for _, cue := range cues {
			if strings.Contains(blob, cue) {
				return true
			}
		}
	}
	return false
}

func productTooHeavyForAcneFace(name, brand, category, notes string) bool {
	blob := strings.ToLower(strings.Join([]string{name, brand, category, notes}, " "))
	for _, skip := range []string{
		"foaming", "cleanser", "cleansing", "face wash",
		"sữa rửa", "sua rua", "rửa mặt", "rua mat", "gel rửa",
		"spf", "sunscreen", "chống nắng", "chong nang",
	} {
		if strings.Contains(blob, skip) {
			return false
		}
	}
	for _, cue := range []string{
		"body butter", "body lotion", "body cream", "body oil",
		"dưỡng thể", "duong the", "kem dưỡng thể", "kem body",
		"bơ dưỡng thể", "bo duong the",
		"coconut oil", "dầu dừa", "dau dua",
		"shea butter", "bơ hạt mỡ",
		"petrolatum", "vaseline", "mineral oil", "dầu khoáng",
	} {
		if strings.Contains(blob, cue) {
			return true
		}
	}
	if strings.Contains(blob, "coconut") && strings.Contains(blob, "butter") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "body", "body butter", "body lotion", "body cream":
		return true
	default:
		return false
	}
}

func wardrobeInsightAnchors(skin, goal string, concerns []string) []string {
	var anchors []string
	addPhrase := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if len([]rune(s)) < 3 {
			return
		}
		anchors = append(anchors, s)
		for _, part := range strings.FieldsFunc(s, func(r rune) bool {
			return r == '/' || r == ',' || r == ';' || r == '|'
		}) {
			for _, w := range strings.Fields(part) {
				w = strings.Trim(w, ".,")
				if len([]rune(w)) < 3 || insightAnchorStopword(w) {
					continue
				}
				anchors = append(anchors, w)
			}
		}
	}
	addPhrase(skin)
	addPhrase(goal)
	for _, c := range concerns {
		addPhrase(c)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(anchors))
	for _, a := range anchors {
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	return out
}

func insightAnchorStopword(w string) bool {
	switch w {
	case "da", "và", "với", "cho", "một", "các", "đang", "mục", "tiêu",
		"hợp", "loại", "này", "của", "trên", "vào", "khi", "nếu", "rất",
		"hơn", "được", "không", "chưa", "nên", "dùng", "tiếp", "theo", "thời", "gian":
		return true
	default:
		return false
	}
}

var insightInsufficientPhrases = []string{
	"chưa có thông tin đầy đủ",
	"chưa có thông tin",
	"không có thông tin",
	"chưa đủ thông tin",
	"không đủ thông tin",
}

func insightTextInsufficient(s string) bool {
	s = strings.ToLower(s)
	for _, p := range insightInsufficientPhrases {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func insightCardTexts(card dto.WardrobeProductInsight) []string {
	out := []string{card.WhatItDoes, card.Fit.Reason, card.Buy.Why}
	for _, a := range card.Actives {
		out = append(out, a.Name, a.Gloss)
	}
	return out
}

func cardMentionsIrritation(card dto.WardrobeProductInsight) bool {
	blob := strings.ToLower(card.Fit.Reason + " " + card.Buy.Why)
	for _, cue := range []string{"rát", "kích ứng", "châm chích", "bong tróc", "nóng rát"} {
		if strings.Contains(blob, cue) {
			return true
		}
	}
	return false
}

func insightReasonAnchored(reason string, anchors []string) bool {
	r := strings.ToLower(reason)
	for _, a := range anchors {
		if a != "" && strings.Contains(r, a) {
			return true
		}
	}
	return false
}

// validateWardrobeProductInsight reports rule breaks on an already parsed card.
// An empty list means the card can be stored.
func validateWardrobeProductInsight(card dto.WardrobeProductInsight, facts wardrobeInsightFacts) []string {
	if !facts.SkinKnown {
		return nil
	}
	var problems []string
	if facts.SkinTypeKnown {
		for _, text := range insightCardTexts(card) {
			if insightTextInsufficient(text) {
				problems = append(problems, "Skin type is known. Do not say there is not enough information about the skin (chưa có thông tin đầy đủ, không có thông tin, chưa đủ thông tin).")
				break
			}
		}
		if card.Fit.Verdict == dto.WardrobeFitNo && !insightReasonAnchored(card.Fit.Reason, facts.Anchors) {
			problems = append(problems, "fit is no, so fit.reason must name this person's skin type, a concern, or the goal, and say the concrete mismatch.")
		}
	}
	if facts.HeavyForFace && facts.AcneProne {
		if card.Fit.Verdict != dto.WardrobeFitNo || card.Buy.Advice != dto.WardrobeBuyNo {
			problems = append(problems, "This is a body product or a heavy oil/occlusive for acne-prone skin. fit.verdict must be no and buy.advice must be \"chưa nên\", with a reason that it is too heavy or too occlusive for this person's skin.")
		}
	}
	keepish := card.Fit.Verdict == dto.WardrobeFitYes || card.Fit.Verdict == dto.WardrobeFitMaybe
	if keepish && card.Buy.Advice == dto.WardrobeBuyNo && !cardMentionsIrritation(card) {
		problems = append(problems, "fit is yes or maybe, so buy.advice must be \"nên mua\" (keep using) unless fit.reason or buy.why names current irritation (rát, kích ứng). Do not pair a possible fit with \"chưa nên\" just because a check-in is missing.")
	}
	return problems
}

func wardrobeInsightCorrection(problems []string) string {
	var b strings.Builder
	b.WriteString("The previous JSON broke the cabinet rules. Return one new JSON object only, same keys. buy.advice must be exactly \"nên mua\" or \"chưa nên\". Do not use the word mua in any other string.\n")
	for _, p := range problems {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	return b.String()
}

// wardrobeInsightFallback is the card used when the model contradicts itself
// twice. The sentences stay consistent with each other and name the saved profile.
func wardrobeInsightFallback(facts wardrobeInsightFacts) dto.WardrobeProductInsight {
	switch {
	case facts.HeavyForFace && facts.AcneProne:
		return heavyAcneFallback(facts)
	case facts.Irritated:
		return irritationFallback(facts)
	case facts.SkinTypeKnown:
		return keepUsingFallback(facts)
	default:
		return unknownSkinFallback()
	}
}

func heavyAcneFallback(facts wardrobeInsightFacts) dto.WardrobeProductInsight {
	skin := facts.SkinType
	if skin == "" {
		skin = "da dễ nổi mụn"
	}
	var reason string
	switch {
	case facts.Goal != "":
		reason = fmt.Sprintf("Kết cấu nặng và bí. Với %s đang muốn %s, bôi lên mặt dễ làm lỗ chân lông bị bít.", skin, lowerFirst(facts.Goal))
	case len(facts.Concerns) > 0:
		reason = fmt.Sprintf("Kết cấu nặng và bí. Với %s đang bận tâm %s, bôi lên mặt dễ làm lỗ chân lông bị bít.", skin, joinVietnamese(facts.Concerns))
	default:
		reason = fmt.Sprintf("Kết cấu nặng và bí. Với %s, bôi lên mặt dễ làm lỗ chân lông bị bít.", skin)
	}
	why := fmt.Sprintf("Chưa nên dùng tiếp trên mặt vì kết cấu nặng, không hợp %s.", skin)
	if facts.Goal != "" {
		why = fmt.Sprintf("Chưa nên dùng tiếp trên mặt vì kết cấu nặng, không hợp %s đang muốn %s.", skin, lowerFirst(facts.Goal))
	}
	return dto.WardrobeProductInsight{
		WhatItDoes: "Sản phẩm đặc và bí, dùng cho da trên cơ thể hơn là da mặt.",
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitNo, Reason: reason},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: why},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
}

func irritationFallback(facts wardrobeInsightFacts) dto.WardrobeProductInsight {
	who := "Da"
	if facts.SkinType != "" {
		who = upperFirst(strings.TrimSpace(facts.SkinType))
	}
	return dto.WardrobeProductInsight{
		WhatItDoes: "Sản phẩm đang có trong tủ đồ.",
		Fit: dto.WardrobeProductFit{
			Verdict: dto.WardrobeFitNo,
			Reason:  who + " đang có dấu hiệu rát hoặc kích ứng gần đây, nên tạm dừng sản phẩm này.",
		},
		Buy: dto.WardrobeProductBuy{
			Advice: dto.WardrobeBuyNo,
			Why:    "Chưa nên dùng tiếp vì da đang rát hoặc kích ứng.",
		},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
}

func keepUsingFallback(facts wardrobeInsightFacts) dto.WardrobeProductInsight {
	head := upperFirst(strings.TrimSpace(facts.SkinType))
	switch {
	case len(facts.Concerns) > 0 && facts.Goal != "":
		head = fmt.Sprintf("%s, đang quan tâm %s, mục tiêu %s", head, joinVietnamese(facts.Concerns), lowerFirst(facts.Goal))
	case len(facts.Concerns) > 0:
		head = fmt.Sprintf("%s, đang quan tâm %s", head, joinVietnamese(facts.Concerns))
	case facts.Goal != "":
		head = fmt.Sprintf("%s, mục tiêu %s", head, lowerFirst(facts.Goal))
	}
	why := "Có thể dùng tiếp và theo dõi, vì chưa có dấu hiệu kích ứng."
	if facts.SkinType != "" && facts.Goal != "" {
		why = fmt.Sprintf("Có thể dùng tiếp và theo dõi, vì %s với mục tiêu %s và chưa có dấu hiệu kích ứng.", facts.SkinType, lowerFirst(facts.Goal))
	} else if facts.SkinType != "" {
		why = fmt.Sprintf("Có thể dùng tiếp và theo dõi, vì %s và chưa có dấu hiệu kích ứng.", facts.SkinType)
	}
	return dto.WardrobeProductInsight{
		WhatItDoes: "Sản phẩm đang có trong tủ đồ.",
		Fit: dto.WardrobeProductFit{
			Verdict: dto.WardrobeFitMaybe,
			Reason:  head + ". Chưa thấy da đang rát, có thể dùng tiếp và theo dõi.",
		},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyYes, Why: why},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
}

func unknownSkinFallback() dto.WardrobeProductInsight {
	const what = "Sản phẩm đang có trong tủ đồ."
	raw := []byte(`{"what_it_does":"` + what + `","fit":{"verdict":"yes","reason":"x"},"buy":{"advice":"nên mua","why":"x"}}`)
	card, err := dto.ParseWardrobeProductInsight(raw, false)
	if err != nil {
		return dto.WardrobeProductInsight{
			WhatItDoes: what,
			Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitMaybe, Reason: "Chưa có loại da hoặc check-in gần đây để so."},
			Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: "Chưa đủ thông tin da để biết có nên dùng tiếp."},
			Disclaimer: dto.WardrobeInsightDisclaimer,
		}
	}
	return card
}

func joinVietnamese(parts []string) string {
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			clean = append(clean, p)
		}
	}
	switch len(clean) {
	case 0:
		return ""
	case 1:
		return clean[0]
	case 2:
		return clean[0] + " và " + clean[1]
	default:
		return strings.Join(clean[:len(clean)-1], ", ") + " và " + clean[len(clean)-1]
	}
}

func lowerFirst(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) == 0 {
		return ""
	}
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func snapshotMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func snapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func snapStrings(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		s, ok := x.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func barrierCompromised(raw json.RawMessage) bool {
	m := snapshotMap(raw)
	if m == nil {
		return false
	}
	if strings.EqualFold(snapString(m, "barrier_signal"), "possibly_compromised") {
		return true
	}
	analysis, _ := m["skin_analysis"].(map[string]any)
	if analysis == nil {
		return false
	}
	s, _ := analysis["barrier_signal"].(string)
	return strings.EqualFold(strings.TrimSpace(s), "possibly_compromised")
}
