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
	// FaceLimit is set when this owned product should not be put on acne-prone facial skin.
	// Body-labeled products and coconut oil only. Petrolatum, vaseline, and mineral oil are not limited here.
	FaceLimit   acneFaceLimit
	ProductName string
	Brand       string
	Category    string
	// ClogProne is the user's skin, not the product: mụn ẩn, lỗ chân lông bít, comedones.
	ClogProne bool
}

// acneFaceLimit is why an owned product should stay off an acne-prone face.
type acneFaceLimit int

const (
	acneFaceOK acneFaceLimit = iota
	acneFaceBodyProduct
	acneFaceCoconutOil
)

func assembleWardrobeInsightFacts(req WardrobeProductInsightRequest) wardrobeInsightFacts {
	var facts wardrobeInsightFacts
	facts.ProductName = strings.TrimSpace(req.Name)
	facts.Brand = strings.TrimSpace(req.Brand)
	facts.Category = strings.TrimSpace(req.Category)
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
	facts.ClogProne = wardrobeClogProne(notes, strings.Join(facts.Concerns, " "), strings.Join(concernRaws, " "), goalRaw, facts.Goal)
	facts.Irritated = recentShowsIrritation(limitRecentForInsight(req.Recent))
	facts.FaceLimit = acneFaceUseLimit(req.Name, req.Brand, req.Category, req.Notes)

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
	if facts.AcneProne && facts.FaceLimit == acneFaceBodyProduct {
		fmt.Fprintf(&b, "\nPRODUCT_FIT_HINT: this product is labeled for body use, and this person's face is acne-prone. fit.verdict must be no. buy.advice must be \"chưa nên\". It is fine for body use. Hedge with \"có thể chưa hợp\", \"có thể khiến\", or \"bạn cân nhắc\". Do not command them (no \"không nên bôi lên mặt\"). Do not say the product is bad, nặng, or occlusive. You may say it is khá đặc and có thể bít lỗ chân lông. Include the word \"mặt\" in fit.reason or buy.why. Do not write a brand name. what_it_does must be %q. fit.reason must be %q. buy.why must be %q.\n",
			exampleBodyWhat, bodyFaceReason(facts), exampleBodyWhy)
	}
	if facts.AcneProne && facts.FaceLimit == acneFaceCoconutOil {
		fmt.Fprintf(&b, "\nPRODUCT_FIT_HINT: this is coconut oil (dầu dừa), and this person's face is acne-prone. fit.verdict must be no. buy.advice must be \"chưa nên\". Hedge with \"có thể chưa hợp\", \"có thể khiến\", or \"bạn cân nhắc\". Do not command them (no \"không nên bôi lên mặt\"). Do not say the oil is bad, nặng, or occlusive. You may say it is khá đặc and có thể bít lỗ chân lông. Include the word \"mặt\" in fit.reason or buy.why. Do not write a brand name. what_it_does must be %q. fit.reason must be %q. buy.why must be %q. actives must include one item named %q with gloss %q.\n",
			exampleCoconutWhat, coconutFaceReason(facts), exampleCoconutWhy, coconutActiveName, exampleCoconutGloss)
	}
	if goalOnlyCardRequired(facts) {
		fmt.Fprintf(&b, "\nPRODUCT_FIT_HINT: only the goal is known; skin type is not on file. fit.verdict must be maybe. buy.advice must be \"chưa nên\". Do not invent a skin type. fit.reason must be %q. buy.why must be %q. \"bí da\" here is the skin feeling, not a word for the product.\n",
			goalOnlyFitReasonFor(facts), goalOnlyBuyWhy)
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

// wardrobeClogProne is about the user's skin (clogged pores, closed comedones),
// not about a product that might clog pores.
func wardrobeClogProne(parts ...string) bool {
	blob := strings.ToLower(strings.Join(parts, " "))
	for _, cue := range []string{"mụn ẩn", "mun an", "bít tắc", "bit tac", "comedone", "clogged pore", "lỗ chân lông bít"} {
		if strings.Contains(blob, cue) {
			return true
		}
	}
	return false
}

// Cabinet card examples. The system prompt, the per-profile hint, the retry
// instruction, and the fallback card all use these sentences, so a line the
// model is told to copy is a line the checker accepts.
const (
	exampleKeepReason   = "Phù hợp với da hỗn hợp và mục tiêu giảm mụn."
	exampleKeepWhy      = "Nên dùng tiếp vì hợp với da dầu và mục tiêu giảm mụn."
	examplePauseWhy     = "Có thể chưa hợp lúc này vì da đang rát, bạn cân nhắc tạm dừng."
	exampleCoconutWhat  = "Dầu dừa thường dùng để dưỡng ẩm cho da."
	exampleCoconutWhy   = "Bạn cân nhắc tạm dừng trên da mặt, vì dầu dừa khá đặc, dễ bít lỗ chân lông, nên có thể chưa hợp."
	exampleCoconutGloss = "dưỡng ẩm, nhưng khá đặc nên có thể bít lỗ chân lông trên da mặt"
	coconutActiveName   = "Dầu dừa"
	exampleBodyWhat     = "Kem dưỡng cho cơ thể thường dùng để dưỡng ẩm cho da."
	exampleBodyWhy      = "Bạn cân nhắc tạm dừng trên da mặt, còn dùng cho cơ thể thì được."
	exampleCleanserWhat = "Sữa rửa mặt tạo bọt, giúp làm sạch dầu thừa, dành cho da dầu và dễ nổi mụn."
	exampleGenericWhat  = "Sản phẩm này thường dùng để chăm da hằng ngày."

	// Spec sentence for the combination, acne-prone profile. coconutFaceReason
	// returns this when the saved skin type is da hỗn hợp.
	exampleCoconutReason = "Dầu dừa khá đặc, dễ bít lỗ chân lông, nên có thể chưa hợp với da mặt hỗn hợp dễ nổi mụn của bạn."
	exampleBodyReason    = "Kem dưỡng cho cơ thể khá đặc, dễ bít lỗ chân lông, nên có thể chưa hợp với da mặt hỗn hợp dễ nổi mụn của bạn."

	// Goal known, skin type not on file, and this product is not kept off an
	// acne-prone face. The why is shared. The reason depends on the product.
	goalOnlyFitReason     = "Chưa rõ loại da của bạn. Cứ dùng và để ý da vài tuần, thấy khô rát hay nổi mụn thêm thì tạm dừng."
	goalOnlyCoconutReason = "Chưa rõ loại da của bạn. Dầu dừa khá đặc, nếu bôi mặt thì thử một vùng nhỏ trước, thấy bí da hay nổi mụn thì tạm dừng."
	goalOnlyBodyReason    = "Chưa rõ loại da của bạn. Kem dưỡng thể thường đặc hơn kem dưỡng mặt, nếu bôi mặt thì thử một vùng nhỏ trước, thấy bí da hay nổi mụn thì tạm dừng."
	goalOnlyBuyWhy        = "Chưa chắc, vì app chưa biết loại da của bạn. Soi da một lần để app trả lời rõ hơn."
)

func acneFacePhrase(facts wardrobeInsightFacts) string {
	skin := strings.TrimSpace(strings.ToLower(facts.SkinType))
	skin = strings.TrimPrefix(skin, "da ")
	skin = strings.TrimSpace(skin)
	switch {
	case skin != "" && (facts.AcneProne || facts.ClogProne):
		return "da mặt " + skin + " dễ nổi mụn"
	case skin != "":
		return "da mặt " + skin
	case len(facts.Concerns) > 0:
		return "da mặt dễ nổi mụn (" + joinVietnamese(facts.Concerns) + ")"
	default:
		return "da mặt dễ nổi mụn"
	}
}

// faceLimitEnding is the clause after "nên". When the skin type is missing and
// a goal is known, the goal fills "nếu bạn đang muốn …". The skin-type sentence
// stays the approved one.
func faceLimitEnding(facts wardrobeInsightFacts) string {
	if strings.TrimSpace(facts.SkinType) == "" && strings.TrimSpace(facts.Goal) != "" {
		return "có thể chưa hợp nếu bạn đang muốn " + lowerFirst(facts.Goal) + "."
	}
	return "có thể chưa hợp với " + acneFacePhrase(facts) + " của bạn."
}

func coconutFaceReason(facts wardrobeInsightFacts) string {
	return "Dầu dừa khá đặc, dễ bít lỗ chân lông, nên " + faceLimitEnding(facts)
}

func bodyFaceReason(facts wardrobeInsightFacts) string {
	return "Kem dưỡng cho cơ thể khá đặc, dễ bít lỗ chân lông, nên " + faceLimitEnding(facts)
}

func fallbackWhatItDoes(facts wardrobeInsightFacts) string {
	if facts.FaceLimit == acneFaceCoconutOil {
		return exampleCoconutWhat
	}
	if facts.FaceLimit == acneFaceBodyProduct {
		return exampleBodyWhat
	}
	blob := strings.ToLower(strings.Join([]string{facts.ProductName, facts.Category}, " "))
	switch {
	case isFoamingCleanser(blob):
		return exampleCleanserWhat
	case isCleanserText(blob):
		return "Sữa rửa mặt thường dùng để làm sạch bụi và dầu thừa trên da."
	case containsAny(blob, "chống nắng", "chong nang", "sunscreen", "spf"):
		return "Kem chống nắng thường dùng để bảo vệ da khỏi nắng."
	case containsAny(blob, "serum", "tinh chất", "tinh chat"):
		return "Serum thường dùng để dưỡng thêm cho da."
	case containsAny(blob, "kem dưỡng", "kem duong", "moisturizer", "dưỡng ẩm", "duong am"):
		return "Kem dưỡng thường dùng để dưỡng ẩm cho da."
	default:
		return exampleGenericWhat
	}
}

func isFoamingCleanser(blob string) bool {
	if !containsAny(blob, "foaming", "tạo bọt", "tao bot") {
		return false
	}
	return isCleanserText(blob) || containsAny(blob, "gel", "foam")
}

func isCleanserText(blob string) bool {
	return containsAny(blob,
		"cleanser", "cleansing", "face wash", "foaming",
		"sữa rửa", "sua rua", "rửa mặt", "rua mat", "gel rửa",
	)
}

func whatItDoesHasPurpose(what string) bool {
	w := strings.ToLower(what)
	for _, cue := range []string{
		"thường dùng", "dùng để", "giúp ", "dành cho", "dưỡng ẩm", "làm sạch",
		"bảo vệ", "làm dịu", "khóa ẩm", "chăm da",
	} {
		if strings.Contains(w, cue) {
			return true
		}
	}
	return false
}

func insightSkinTypeAsEffect(blob string) bool {
	blob = strings.ToLower(blob)
	for _, p := range []string{
		"làm sạch da dầu",
		"làm sạch da khô",
		"làm sạch da hỗn hợp",
		"làm sạch da nhạy",
		"làm sạch da mụn",
		"khiến da dễ nổi mụn",
		"làm da dễ nổi mụn",
		"làm cho da dễ nổi mụn",
	} {
		if strings.Contains(blob, p) {
			return true
		}
	}
	return false
}

func insightRewritesGoal(blob string) bool {
	blob = strings.ToLower(blob)
	for _, p := range []string{"làm sạch mụn", "trị mụn", "hết mụn"} {
		if strings.Contains(blob, p) {
			return true
		}
	}
	return false
}

func insightCommandsUser(blob string) bool {
	blob = strings.ToLower(blob)
	for _, p := range []string{"không nên bôi", "đừng bôi", "phải ngừng", "cấm bôi", "cấm dùng"} {
		if strings.Contains(blob, p) {
			return true
		}
	}
	return false
}

// insightCallsProductHeavy rejects words that call the product bad.
// "bí" is its own word. It must not match inside "bít". "bí da" is the skin
// feeling in the goal-only trial line, not a label on the product.
func insightCallsProductHeavy(blob string) bool {
	blob = strings.ToLower(blob)
	if strings.Contains(blob, "nặng") || strings.Contains(blob, "occlusive") {
		return true
	}
	blob = strings.ReplaceAll(blob, "bí da", " ")
	return hasVietnameseWord(blob, "bí")
}

func hasVietnameseWord(blob, word string) bool {
	blob = strings.ToLower(blob)
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return false
	}
	b := []rune(blob)
	w := []rune(word)
	for i := 0; i+len(w) <= len(b); i++ {
		if i > 0 && unicode.IsLetter(b[i-1]) {
			continue
		}
		match := true
		for j := range w {
			if b[i+j] != w[j] {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		end := i + len(w)
		if end < len(b) && unicode.IsLetter(b[end]) {
			continue
		}
		return true
	}
	return false
}

func cardEchoesBrand(card dto.WardrobeProductInsight, brand string) bool {
	brand = strings.TrimSpace(brand)
	// Three ASCII letters in a row skips Vietnamese product words such as "Dầu dừa",
	// which the card is supposed to say, and still catches CeraVe, Nivea, The Body Shop.
	if !looksLikeBrandToken(brand) {
		return false
	}
	blob := strings.ToLower(strings.Join(insightCardTexts(card), " "))
	return strings.Contains(blob, strings.ToLower(brand))
}

func looksLikeBrandToken(s string) bool {
	run := 0
	for _, r := range s {
		if r <= unicode.MaxASCII && unicode.IsLetter(r) {
			run++
			if run >= 3 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

func poreTalkOK(card dto.WardrobeProductInsight, facts wardrobeInsightFacts) bool {
	blob := strings.ToLower(card.Fit.Reason + " " + card.Buy.Why)
	if insightCallsProductHeavy(blob) {
		return false
	}
	if !strings.Contains(blob, "bít") {
		return true
	}
	if !strings.Contains(blob, "có thể") {
		return false
	}
	return facts.AcneProne || facts.ClogProne
}

func activeGlossPoreOK(card dto.WardrobeProductInsight) bool {
	for _, a := range card.Actives {
		g := strings.ToLower(a.Name + " " + a.Gloss)
		if strings.Contains(g, "bít") && !strings.Contains(strings.ToLower(a.Gloss), "có thể") {
			return false
		}
		if insightCallsProductHeavy(g) || insightSkinTypeAsEffect(g) || insightRewritesGoal(g) {
			return false
		}
	}
	return true
}

func coconutActiveOK(card dto.WardrobeProductInsight) bool {
	for _, a := range card.Actives {
		name := strings.ToLower(a.Name)
		gloss := strings.ToLower(a.Gloss)
		if (strings.Contains(name, "dừa") || strings.Contains(name, "coconut")) &&
			strings.Contains(gloss, "dưỡng") &&
			strings.Contains(gloss, "có thể") &&
			strings.Contains(gloss, "bít") &&
			strings.Contains(gloss, "lỗ chân lông") {
			return true
		}
	}
	return false
}

func negativeLineHedged(card dto.WardrobeProductInsight) bool {
	if card.Fit.Verdict != dto.WardrobeFitNo && card.Buy.Advice != dto.WardrobeBuyNo {
		return true
	}
	blob := strings.ToLower(card.Fit.Reason + " " + card.Buy.Why)
	if insightCommandsUser(blob) {
		return false
	}
	return strings.Contains(blob, "có thể") || strings.Contains(blob, "bạn cân nhắc") || strings.Contains(blob, "chưa chắc")
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

// acneFaceUseLimit reports when an owned product should stay off an acne-prone face.
// Body-labeled products (body lotion, body butter, body cream, dưỡng thể, kem body)
// and coconut oil (dầu dừa) are limited. Petrolatum, vaseline, and mineral oil are not.
// There is no rule aimed at one product name.
func acneFaceUseLimit(name, brand, category, notes string) acneFaceLimit {
	blob := strings.ToLower(strings.Join([]string{name, brand, category, notes}, " "))
	for _, skip := range []string{
		"foaming", "cleanser", "cleansing", "face wash",
		"sữa rửa", "sua rua", "rửa mặt", "rua mat", "gel rửa",
		"spf", "sunscreen", "chống nắng", "chong nang",
	} {
		if strings.Contains(blob, skip) {
			return acneFaceOK
		}
	}
	for _, cue := range []string{
		"body butter", "body lotion", "body cream", "body oil",
		"dưỡng thể", "duong the", "kem dưỡng thể", "kem body",
		"bơ dưỡng thể", "bo duong the",
	} {
		if strings.Contains(blob, cue) {
			return acneFaceBodyProduct
		}
	}
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "body", "body butter", "body lotion", "body cream":
		return acneFaceBodyProduct
	}
	for _, cue := range []string{"coconut oil", "dầu dừa", "dau dua"} {
		if strings.Contains(blob, cue) {
			return acneFaceCoconutOil
		}
	}
	return acneFaceOK
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
	problems = append(problems, insightCopyProblems(card, facts)...)
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
	if facts.AcneProne && facts.FaceLimit != acneFaceOK {
		if card.Fit.Verdict != dto.WardrobeFitNo || card.Buy.Advice != dto.WardrobeBuyNo || !faceLimitReasonOK(card, facts.FaceLimit) {
			problems = append(problems, faceLimitProblem(facts))
		}
	}
	if goalOnlyCardRequired(facts) {
		reason := goalOnlyFitReasonFor(facts)
		if card.Fit.Verdict != dto.WardrobeFitMaybe || card.Buy.Advice != dto.WardrobeBuyNo || card.Fit.Reason != reason || card.Buy.Why != goalOnlyBuyWhy {
			problems = append(problems, fmt.Sprintf(`Skin type is not on file and only the goal is known. fit.verdict must be maybe and buy.advice must be "chưa nên". fit.reason must be %q. buy.why must be %q.`, reason, goalOnlyBuyWhy))
		}
	}
	if !negativeLineHedged(card) {
		problems = append(problems, `Negative fit.reason and buy.why must hedge with "có thể chưa hợp", "có thể khiến", or "bạn cân nhắc". Do not command the user ("không nên bôi", "đừng bôi", "phải ngừng").`)
	}
	keepish := card.Fit.Verdict == dto.WardrobeFitYes || card.Fit.Verdict == dto.WardrobeFitMaybe
	// Skin type not on file is already required to be maybe + "chưa nên".
	// That pairing is only a contradiction once the skin type itself is known.
	if facts.SkinTypeKnown && keepish && card.Buy.Advice == dto.WardrobeBuyNo && !cardMentionsIrritation(card) {
		problems = append(problems, "fit is yes or maybe, so buy.advice must be \"nên mua\" (keep using) unless fit.reason or buy.why names current irritation (rát, kích ứng). Do not pair a possible fit with \"chưa nên\" just because a check-in is missing.")
	}
	return problems
}

func insightCopyProblems(card dto.WardrobeProductInsight, facts wardrobeInsightFacts) []string {
	var problems []string
	if !whatItDoesHasPurpose(card.WhatItDoes) {
		problems = append(problems, `what_it_does must be one full short sentence saying what kind of product it is and what it is for, never only the name. Example: "`+exampleCoconutWhat+`".`)
	}
	blob := strings.ToLower(strings.Join(insightCardTexts(card), " "))
	if insightSkinTypeAsEffect(blob) {
		problems = append(problems, `Skin-type words describe this person's skin or who the product is for, never what the product does. Write "dành cho da dầu và dễ nổi mụn", not "giúp làm sạch da dầu và dễ nổi mụn".`)
	}
	if insightRewritesGoal(blob) {
		problems = append(problems, `When you name the goal, copy it exactly. Write "mục tiêu giảm mụn". Do not write "làm sạch mụn", "trị mụn", or "hết mụn".`)
	}
	if cardEchoesBrand(card, facts.Brand) {
		problems = append(problems, "Do not write a brand name in any string. Describe the product by what it is (dầu dừa, sữa rửa mặt, kem dưỡng).")
	}
	if !poreTalkOK(card, facts) {
		problems = append(problems, `You may say "khá đặc, dễ bít lỗ chân lông" only together with "có thể", and only when this person is acne-prone (dễ nổi mụn) or gets clogged pores. Do not say nặng, bí, or occlusive. Do not call the product bad.`)
	}
	if !activeGlossPoreOK(card) {
		problems = append(problems, `An ingredient gloss may say "có thể bít lỗ chân lông" only with the word "có thể". Do not say nặng, bí, or occlusive.`)
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
	case facts.AcneProne && facts.FaceLimit != acneFaceOK:
		return faceLimitFallback(facts)
	case facts.Irritated:
		return irritationFallback(facts)
	case facts.SkinTypeKnown:
		return keepUsingFallback(facts)
	case strings.TrimSpace(facts.Goal) != "":
		return goalOnlyFallback(facts)
	default:
		return unknownSkinFallback(facts)
	}
}

// goalOnlyCardRequired is a profile with a goal, no skin type, and no
// acne-prone face limit. Irritation still uses its own card.
func goalOnlyCardRequired(facts wardrobeInsightFacts) bool {
	if facts.SkinTypeKnown || facts.Irritated || strings.TrimSpace(facts.Goal) == "" {
		return false
	}
	if facts.AcneProne && facts.FaceLimit != acneFaceOK {
		return false
	}
	return true
}

// goalOnlyFitReasonFor picks the maybe-card reason. "Cứ dùng" is only the
// foaming cleanser. Coconut oil and a body product each have their own line.
// Any other product keeps the cleanser line.
func goalOnlyFitReasonFor(facts wardrobeInsightFacts) string {
	switch facts.FaceLimit {
	case acneFaceCoconutOil:
		return goalOnlyCoconutReason
	case acneFaceBodyProduct:
		return goalOnlyBodyReason
	default:
		return goalOnlyFitReason
	}
}

// goalOnlyFallback is the card when the profile has a goal and no skin type.
// Verdict stays maybe / "chưa nên" — the same as an unknown skin type.
// The why is shared. The reason depends on the product kind.
func goalOnlyFallback(facts wardrobeInsightFacts) dto.WardrobeProductInsight {
	return dto.WardrobeProductInsight{
		WhatItDoes: fallbackWhatItDoes(facts),
		Fit: dto.WardrobeProductFit{
			Verdict: dto.WardrobeFitMaybe,
			Reason:  goalOnlyFitReasonFor(facts),
		},
		Buy: dto.WardrobeProductBuy{
			Advice: dto.WardrobeBuyNo,
			Why:    goalOnlyBuyWhy,
		},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
}

func faceLimitProblem(facts wardrobeInsightFacts) string {
	if facts.FaceLimit == acneFaceCoconutOil {
		return fmt.Sprintf(`This is coconut oil (dầu dừa) and this person's face is acne-prone. fit.verdict must be no and buy.advice must be "chưa nên". Hedge the reason ("có thể chưa hợp", "có thể khiến", "bạn cân nhắc"). Do not command them ("không nên bôi lên mặt"). Do not say the oil is bad, nặng, or occlusive. You may say khá đặc and có thể bít lỗ chân lông. fit.reason or buy.why must include the word "mặt" and one plain why. what_it_does must be %q. fit.reason must be %q. buy.why must be %q. Include one active named %q with gloss %q. Do not write a brand name.`,
			exampleCoconutWhat, coconutFaceReason(facts), exampleCoconutWhy, coconutActiveName, exampleCoconutGloss)
	}
	return fmt.Sprintf(`This product is labeled for body use and this person's face is acne-prone. fit.verdict must be no and buy.advice must be "chưa nên". Say it is a body product, fine for the body, and that it may not suit their face. Hedge the reason ("có thể chưa hợp", "có thể khiến", "bạn cân nhắc"). Do not command them ("không nên bôi lên mặt"). Do not say the product is bad, nặng, or occlusive. You may say khá đặc and có thể bít lỗ chân lông. fit.reason or buy.why must include the word "mặt" and one plain why. what_it_does must be %q. fit.reason must be %q. buy.why must be %q. Do not write a brand name.`,
		exampleBodyWhat, bodyFaceReason(facts), exampleBodyWhy)
}

func faceLimitReasonOK(card dto.WardrobeProductInsight, limit acneFaceLimit) bool {
	blob := strings.ToLower(card.Fit.Reason + " " + card.Buy.Why)
	if insightCommandsUser(blob) || insightCallsProductHeavy(blob) {
		return false
	}
	// "có thể" is the hedge. "mặt" is required so a reason about "da bạn" cannot
	// sneak past. "bít lỗ chân lông" is the plain why, and only counts when hedged.
	if !strings.Contains(blob, "mặt") || !strings.Contains(blob, "có thể") {
		return false
	}
	if !strings.Contains(blob, "bít") || !strings.Contains(blob, "lỗ chân lông") {
		return false
	}
	if limit == acneFaceCoconutOil {
		if !(strings.Contains(blob, "dừa") || strings.Contains(blob, "coconut")) || !coconutActiveOK(card) {
			return false
		}
		return true
	}
	return strings.Contains(blob, "cơ thể") || strings.Contains(blob, "dưỡng thể") || strings.Contains(blob, "dưỡng thân") || strings.Contains(blob, "body")
}

func faceLimitFallback(facts wardrobeInsightFacts) dto.WardrobeProductInsight {
	reason := bodyFaceReason(facts)
	why := exampleBodyWhy
	if facts.FaceLimit == acneFaceCoconutOil {
		reason = coconutFaceReason(facts)
		why = exampleCoconutWhy
	}
	card := dto.WardrobeProductInsight{
		WhatItDoes: fallbackWhatItDoes(facts),
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitNo, Reason: reason},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: why},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	if facts.FaceLimit == acneFaceCoconutOil {
		card.Actives = []dto.WardrobeProductActive{{
			Name:  coconutActiveName,
			Gloss: exampleCoconutGloss,
		}}
	}
	return card
}

func irritationFallback(facts wardrobeInsightFacts) dto.WardrobeProductInsight {
	who := "Da"
	if facts.SkinType != "" {
		who = upperFirst(strings.TrimSpace(facts.SkinType))
	}
	return dto.WardrobeProductInsight{
		WhatItDoes: fallbackWhatItDoes(facts),
		Fit: dto.WardrobeProductFit{
			Verdict: dto.WardrobeFitNo,
			Reason:  who + " có dấu hiệu rát hoặc kích ứng gần đây, bạn cân nhắc tạm dừng sản phẩm này.",
		},
		Buy: dto.WardrobeProductBuy{
			Advice: dto.WardrobeBuyNo,
			Why:    "Có thể chưa hợp lúc này vì da đang rát hoặc kích ứng, bạn cân nhắc tạm dừng.",
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
		WhatItDoes: fallbackWhatItDoes(facts),
		Fit: dto.WardrobeProductFit{
			Verdict: dto.WardrobeFitMaybe,
			Reason:  head + ". Chưa thấy da đang rát, có thể dùng tiếp và theo dõi.",
		},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyYes, Why: why},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
}

func unknownSkinFallback(facts wardrobeInsightFacts) dto.WardrobeProductInsight {
	what := fallbackWhatItDoes(facts)
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
