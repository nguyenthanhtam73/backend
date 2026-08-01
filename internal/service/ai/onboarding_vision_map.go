package ai

import (
	"strings"
	"unicode"

	"github.com/dadiary/backend/internal/dto"
)

// onboardingVisionRaw mirrors the OpenAI vision JSON schema for onboarding.
type onboardingVisionRaw struct {
	SkinObservations     dto.OnboardingSkinObservations `json:"skin_observations"`
	DetailedObservations string                         `json:"detailed_observations"`
	MainConcerns         []string                       `json:"main_concerns"`
	SkinTone             string                         `json:"skin_tone"`
	Undertone            string                         `json:"undertone"`
	PhotoQuality         string                         `json:"photo_quality"`
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func mapOnboardingVisionRaw(raw onboardingVisionRaw, locale string) dto.OnboardingSkinAnalyzeResponse {
	obs := raw.SkinObservations
	concerns := mapOnboardingConcerns(raw.MainConcerns, obs)
	sufficient, confidence, tips := mapOnboardingPhotoQuality(raw.PhotoQuality, locale)

	return dto.OnboardingSkinAnalyzeResponse{
		SkinTypeGuess:        mapOnboardingSkinType(obs.OverallSkinType),
		UndertoneGuess:       mapOnboardingUndertoneGuess(raw.Undertone, raw.SkinTone),
		Concerns:             concerns,
		SuggestedGoal:        inferOnboardingSuggestedGoal(concerns),
		BarrierSignal:        inferOnboardingBarrierSignal(obs.Redness, obs.Texture),
		Confidence:           confidence,
		VisualObservations:   buildOnboardingVisualObservations(obs, raw.DetailedObservations, locale),
		CoachingNotes:        "",
		PhotoQuality: struct {
			Sufficient bool     `json:"sufficient"`
			Tips       []string `json:"tips"`
		}{Sufficient: sufficient, Tips: tips},
		SkinObservations:     &obs,
		DetailedObservations: strings.TrimSpace(raw.DetailedObservations),
		MainConcerns:         append([]string(nil), raw.MainConcerns...),
		SkinTone:             strings.TrimSpace(raw.SkinTone),
	}
}

// ---------------------------------------------------------------------------
// Direct mapping — vision field → stable profile id
// ---------------------------------------------------------------------------

var onboardingSkinTypeAliases = map[string]string{
	"dry": "dry", "oily": "oily", "normal": "normal", "sensitive": "sensitive",
	"combination": "combo", "combo": "combo",
}

// mapOnboardingSkinType maps vision overall_skin_type to a stable profile id.
func mapOnboardingSkinType(raw string) string {
	if id, ok := onboardingSkinTypeAliases[normLower(raw)]; ok {
		return id
	}
	return "prefer_not"
}

var (
	knownUndertoneIDs = map[string]string{"warm": "warm", "cool": "cool", "neutral": "neutral"}
	skinDepthAliases  = map[string]string{"fair": "fair", "light": "fair", "deep": "deep", "tan": "deep"}
)

// mapOnboardingUndertoneGuess maps vision undertone to profile ids (cool|warm|neutral|fair|deep|prefer_not).
func mapOnboardingUndertoneGuess(undertone, skinTone string) string {
	u := normLower(undertone)
	if id, ok := knownUndertoneIDs[u]; ok {
		return id
	}
	if u == "" || u == "unknown" {
		if depth, ok := skinDepthAliases[normLower(skinTone)]; ok {
			return depth
		}
		return "prefer_not"
	}
	return "prefer_not"
}

// mapOnboardingPhotoQuality maps vision photo_quality to sufficiency, confidence, and locale tips.
func mapOnboardingPhotoQuality(quality, locale string) (sufficient bool, confidence float64, tips []string) {
	switch normLower(quality) {
	case "good":
		return true, 0.85, nil
	case "average":
		tips = onboardingPhotoQualityTips(locale, false)
		return true, 0.65, tips
	case "poor":
		tips = onboardingPhotoQualityTips(locale, true)
		return false, 0.4, tips
	default:
		tips = onboardingPhotoQualityTips(locale, false)
		return true, 0.55, tips
	}
}

func onboardingPhotoQualityTips(locale string, poor bool) []string {
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		if poor {
			return []string{
				"Retake 2–3 face photos in natural daylight, front plus a slight side angle.",
				"Remove heavy filters, keep little or no makeup, and avoid blur or extreme shadows.",
			}
		}
		return []string{
			"For even better reads, use soft natural light and include forehead + cheeks clearly.",
		}
	}
	if poor {
		return []string{
			"Chụp lại 2–3 ảnh mặt đủ sáng (ánh sáng tự nhiên), góc chính diện + nghiêng nhẹ.",
			"Bỏ filter mạnh, hạn chế makeup, tránh ảnh mờ hoặc tối quá.",
		}
	}
	return []string{
		"Để AI đọc da rõ hơn, chụp thêm ánh sáng tự nhiên và lộ rõ trán + má.",
	}
}

// mapOnboardingConcernLabel maps a single vision main_concern label to a stable concern id.
func mapOnboardingConcernLabel(label string) string {
	n := normalizeConcernLabel(label)
	if id, ok := concernLabelAliases[n]; ok {
		return id
	}

	raw := strings.ToLower(strings.TrimSpace(label))
	for _, rule := range concernLabelSubstrings {
		for _, substr := range rule.contains {
			if strings.Contains(raw, substr) {
				return rule.id
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Concern mapping — direct labels + observation inference
// ---------------------------------------------------------------------------

// mapOnboardingConcerns merges vision main_concern labels with concerns inferred from
// structured skin_observations. Main labels keep priority order; observation rules append extras.
func mapOnboardingConcerns(main []string, obs dto.OnboardingSkinObservations) []string {
	collector := newConcernCollector(len(main) + 6)
	for _, label := range main {
		collector.add(mapOnboardingConcernLabel(label))
	}
	inferConcernsFromObservations(collector, obs)
	return collector.ids()
}

type concernCollector struct {
	seen map[string]struct{}
	out  []string
}

func newConcernCollector(capacity int) *concernCollector {
	return &concernCollector{
		seen: make(map[string]struct{}),
		out:  make([]string, 0, capacity),
	}
}

func (c *concernCollector) add(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	if _, ok := c.seen[id]; ok {
		return
	}
	c.seen[id] = struct{}{}
	c.out = append(c.out, id)
}

func (c *concernCollector) ids() []string { return c.out }

var concernLabelAliases = map[string]string{
	"mun viem": "acne", "mun": "acne", "mụn viêm": "acne", "mụn": "acne", "acne": "acne",
	"not do": "acne", "nốt đỏ": "acne", "not sung": "acne", "nốt sưng": "acne",
	"tham nam": "hyperpigmentation", "thâm nám": "hyperpigmentation", "tham": "hyperpigmentation",
	"thâm": "hyperpigmentation", "hyperpigmentation": "hyperpigmentation",
	"da khong deu mau": "hyperpigmentation", "da không đều màu": "hyperpigmentation",
	"da kho": "dryness", "da khô": "dryness", "dryness": "dryness",
	"lo chan long to": "large_pores", "lỗ chân lông to": "large_pores",
	"large pores": "large_pores", "large_pores": "large_pores",
	"da do": "redness", "da đỏ": "redness", "redness": "redness",
	"da de kich ung": "redness", "da dễ kích ứng": "redness",
	"da de do": "weak_barrier", "da dễ đỏ": "weak_barrier",
	"da yeu hon binh thuong": "weak_barrier", "da yếu hơn bình thường": "weak_barrier",
	"barrier yeu": "weak_barrier", "barrier yếu": "weak_barrier",
	"weak barrier": "weak_barrier", "weak_barrier": "weak_barrier",
	"dehydration": "dehydration", "mat nuoc": "dehydration", "mất nước": "dehydration",
	"da thieu am": "dehydration", "da thiếu ẩm": "dehydration",
	"uneven texture": "uneven_texture", "uneven_texture": "uneven_texture",
	"da khong deu": "uneven_texture", "da không đều": "uneven_texture",
	"da hoi san": "uneven_texture", "da hơi sần": "uneven_texture",
}

var concernLabelSubstrings = []struct {
	id       string
	contains []string
}{
	{"acne", []string{"mụn", "mun", "nốt đỏ", "nốt sưng"}},
	{"hyperpigmentation", []string{"thâm", "tham", "nám", "nam", "không đều màu"}},
	{"dryness", []string{"da khô", "khô"}},
	{"large_pores", []string{"chân lông", "chan long", "pore"}},
	{"redness", []string{"dễ kích ứng", "da đỏ", "ửng đỏ"}},
	{"weak_barrier", []string{"barrier", "yếu hơn bình thường", "da dễ đỏ", "da yếu"}},
	{"uneven_texture", []string{"hơi sần", "không mịn", "không đều"}},
}

func normalizeConcernLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// --- Observation status sets (inputs for concern inference rules) ---

type obsValueSet map[string]struct{}

func newObsValueSet(values ...string) obsValueSet {
	set := make(obsValueSet, len(values))
	for _, v := range values {
		set[normLower(v)] = struct{}{}
	}
	return set
}

func (s obsValueSet) has(value string) bool {
	_, ok := s[normLower(value)]
	return ok
}

var (
	obsAcneStatuses          = newObsValueSet("inflammatory_acne", "cystic_acne", "few_whiteheads")
	obsHyperpigmentationVals = newObsValueSet("hyperpigmentation", "dark_spots", "slight_uneven")
	obsRednessLevels         = newObsValueSet("mild", "moderate", "severe")
	obsLargePoreSizes        = newObsValueSet("large", "very_large")
	obsUnevenTextures        = newObsValueSet("slightly_rough", "rough", "bumpy")
	obsHighOilinessLevels    = newObsValueSet("high", "very_high")
)

type observationConcernRule struct {
	id    string
	match func(dto.OnboardingSkinObservations) bool
}

// observationConcernRules maps structured skin_observations cues to stable concern ids.
// Rules run after main_concern labels; large_pores may match via pore_size or oily T-zone.
var observationConcernRules = []observationConcernRule{
	{"acne", func(o dto.OnboardingSkinObservations) bool { return obsAcneStatuses.has(o.AcneStatus) }},
	{"hyperpigmentation", func(o dto.OnboardingSkinObservations) bool { return obsHyperpigmentationVals.has(o.Pigmentation) }},
	{"redness", func(o dto.OnboardingSkinObservations) bool { return obsRednessLevels.has(o.Redness) }},
	{"large_pores", func(o dto.OnboardingSkinObservations) bool { return obsLargePoreSizes.has(o.PoreSize) }},
	{"uneven_texture", func(o dto.OnboardingSkinObservations) bool { return obsUnevenTextures.has(o.Texture) }},
	{"dryness", func(o dto.OnboardingSkinObservations) bool { return normLower(o.OverallSkinType) == "dry" }},
	{"weak_barrier", func(o dto.OnboardingSkinObservations) bool { return normLower(o.OverallSkinType) == "sensitive" }},
	{"large_pores", func(o dto.OnboardingSkinObservations) bool {
		return obsHighOilinessLevels.has(o.OilinessLevel) && normLower(o.TZone) == "very_oily"
	}},
}

// inferConcernsFromObservations appends stable concern ids derived from structured vision cues.
func inferConcernsFromObservations(c *concernCollector, obs dto.OnboardingSkinObservations) {
	for _, rule := range observationConcernRules {
		if rule.match(obs) {
			c.add(rule.id)
		}
	}
}

// ---------------------------------------------------------------------------
// Inference — derived fields from mapped concerns / observation cues
// ---------------------------------------------------------------------------

// inferOnboardingSuggestedGoal picks a default skincare goal from the primary mapped concern.
func inferOnboardingSuggestedGoal(concerns []string) string {
	if len(concerns) == 0 {
		return "unsure"
	}
	switch concerns[0] {
	case "acne":
		return "clear_acne"
	case "hyperpigmentation":
		return "glow"
	case "dryness", "dehydration", "weak_barrier", "redness":
		return "barrier"
	default:
		return "glow"
	}
}

// inferOnboardingBarrierSignal estimates barrier health from redness and texture cues.
func inferOnboardingBarrierSignal(redness, texture string) string {
	switch normLower(redness) {
	case "moderate", "severe":
		return "possibly_compromised"
	case "mild":
		return "unclear"
	}
	switch normLower(texture) {
	case "rough", "bumpy":
		return "possibly_compromised"
	case "slightly_rough":
		return "unclear"
	}
	return "likely_ok"
}

// ---------------------------------------------------------------------------
// Visual observations — structured bullets + free-text sentences
// ---------------------------------------------------------------------------

type visualObsFieldID string

const (
	visualObsTZone        visualObsFieldID = "t_zone"
	visualObsCheeks       visualObsFieldID = "cheeks"
	visualObsPoreSize     visualObsFieldID = "pore_size"
	visualObsTexture      visualObsFieldID = "texture"
	visualObsRedness      visualObsFieldID = "redness"
	visualObsPigmentation visualObsFieldID = "pigmentation"
	visualObsAcneStatus   visualObsFieldID = "acne_status"
	visualObsOiliness     visualObsFieldID = "oiliness_level"
)

// visualObsPlain maps structured enum values → plain user-facing bullets (no English jargon).
var visualObsPlainVI = map[visualObsFieldID]map[string]string{
	visualObsTZone: {
		"dry": "Vùng chữ T (trán–mũi–cằm) hơi khô.", "slightly_oily": "Vùng chữ T (trán–mũi–cằm) hơi bóng.",
		"very_oily": "Vùng chữ T (trán–mũi–cằm) bóng dầu rõ.", "normal": "Vùng chữ T (trán–mũi–cằm) trông ổn.",
	},
	visualObsCheeks: {
		"dry": "Má hơi khô.", "normal": "Má trông ổn.", "slightly_oily": "Má hơi bóng nhẹ.",
	},
	visualObsPoreSize: {
		"small": "Lỗ chân lông nhỏ, ít lộ.", "medium": "Lỗ chân lông vừa phải.",
		"large": "Lỗ chân lông to, nhìn rõ hơn.", "very_large": "Lỗ chân lông rất to trên ảnh.",
	},
	visualObsTexture: {
		"smooth": "Da nhìn khá mịn.", "slightly_rough": "Da hơi sần / không mịn đều.",
		"rough": "Da nhìn sần rõ hơn.", "bumpy": "Da có chỗ gồ ghề nhẹ.",
	},
	visualObsRedness: {
		"mild": "Da ửng đỏ nhẹ.", "moderate": "Da đỏ rõ hơn ở vài vùng.", "severe": "Da đỏ khá rõ trên ảnh.",
	},
	visualObsPigmentation: {
		"slight_uneven": "Da hơi không đều màu.", "hyperpigmentation": "Có thâm / sạm nhìn thấy.",
		"dark_spots": "Có đốm thâm trên ảnh.",
	},
	visualObsAcneStatus: {
		"few_whiteheads": "Có vài nốt nhỏ kiểu đầu trắng.", "inflammatory_acne": "Có nốt đỏ / mụn viêm nhìn thấy.",
		"cystic_acne": "Có nốt sưng sâu trông khó chịu.",
	},
	visualObsOiliness: {
		"low": "Da ít bóng dầu.", "medium": "Da bóng dầu mức vừa.",
		"high": "Da bóng dầu khá rõ.", "very_high": "Da bóng dầu rất rõ trên ảnh.",
	},
}

var visualObsPlainEN = map[visualObsFieldID]map[string]string{
	visualObsTZone: {
		"dry": "The T-zone (forehead–nose–chin) looks a bit dry.", "slightly_oily": "The T-zone looks slightly shiny.",
		"very_oily": "The T-zone looks clearly oily.", "normal": "The T-zone looks balanced.",
	},
	visualObsCheeks: {
		"dry": "Cheeks look a bit dry.", "normal": "Cheeks look fine.", "slightly_oily": "Cheeks look slightly shiny.",
	},
	visualObsPoreSize: {
		"small": "Pores look small.", "medium": "Pores look medium.",
		"large": "Pores look enlarged.", "very_large": "Pores look very enlarged in the photos.",
	},
	visualObsTexture: {
		"smooth": "Skin looks fairly smooth.", "slightly_rough": "Skin looks a bit uneven / not fully smooth.",
		"rough": "Skin looks clearly uneven.", "bumpy": "Skin looks a bit bumpy in places.",
	},
	visualObsRedness: {
		"mild": "Mild redness is visible.", "moderate": "Redness is clearer in a few areas.", "severe": "Redness is quite obvious in the photos.",
	},
	visualObsPigmentation: {
		"slight_uneven": "Tone looks slightly uneven.", "hyperpigmentation": "Dark spots / discoloration are visible.",
		"dark_spots": "Dark spots show up in the photos.",
	},
	visualObsAcneStatus: {
		"few_whiteheads": "A few small whitehead-like bumps.", "inflammatory_acne": "Red inflamed spots are visible.",
		"cystic_acne": "Deeper swollen spots look uncomfortable.",
	},
	visualObsOiliness: {
		"low": "Oiliness looks low.", "medium": "Oiliness looks moderate.",
		"high": "Oiliness looks quite clear.", "very_high": "Oiliness looks very clear in the photos.",
	},
}

type visualObsField struct {
	id         visualObsFieldID
	value      func(dto.OnboardingSkinObservations) string
	skipValues map[string]struct{}
}

var visualObsFields = []visualObsField{
	{visualObsTZone, func(o dto.OnboardingSkinObservations) string { return o.TZone }, nil},
	{visualObsCheeks, func(o dto.OnboardingSkinObservations) string { return o.Cheeks }, nil},
	{visualObsPoreSize, func(o dto.OnboardingSkinObservations) string { return o.PoreSize }, nil},
	{visualObsTexture, func(o dto.OnboardingSkinObservations) string { return o.Texture }, nil},
	{visualObsRedness, func(o dto.OnboardingSkinObservations) string { return o.Redness }, map[string]struct{}{"none": {}}},
	{visualObsPigmentation, func(o dto.OnboardingSkinObservations) string { return o.Pigmentation }, map[string]struct{}{"even": {}}},
	{visualObsAcneStatus, func(o dto.OnboardingSkinObservations) string { return o.AcneStatus }, map[string]struct{}{"clear": {}}},
	{visualObsOiliness, func(o dto.OnboardingSkinObservations) string { return o.OilinessLevel }, nil},
}

// friendlyVisualObsBullet turns a structured enum cue into a plain-language bullet for the UI.
func friendlyVisualObsBullet(id visualObsFieldID, rawValue, locale string) string {
	value := normLower(rawValue)
	table := visualObsPlainVI
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		table = visualObsPlainEN
	}
	if byField, ok := table[id]; ok {
		if s, ok := byField[value]; ok {
			return s
		}
	}
	return ""
}

// buildStructuredVisualBullets formats structured skin_observations as plain-language bullets.
func buildStructuredVisualBullets(obs dto.OnboardingSkinObservations, locale string) []string {
	bullets := make([]string, 0, len(visualObsFields))
	for _, field := range visualObsFields {
		value := field.value(obs)
		if value == "" {
			continue
		}
		if _, skip := field.skipValues[normLower(value)]; skip {
			continue
		}
		if line := friendlyVisualObsBullet(field.id, value, locale); line != "" {
			bullets = append(bullets, line)
		}
	}
	return bullets
}

// buildOnboardingVisualObservations assembles UI bullets from structured cues and free text.
func buildOnboardingVisualObservations(obs dto.OnboardingSkinObservations, detailed, locale string) []string {
	bullets := buildStructuredVisualBullets(obs, locale)
	for _, part := range splitObservationSentences(detailed) {
		bullets = appendTrimmed(bullets, part)
	}
	detailed = strings.TrimSpace(detailed)
	if len(bullets) < 4 && detailed != "" {
		bullets = appendTrimmed(bullets, detailed)
	}
	return bullets
}

// ---------------------------------------------------------------------------
// Text utilities
// ---------------------------------------------------------------------------

var (
	newlineNormalizer        = strings.NewReplacer("\r\n", "\n", "\r", "\n")
	sentenceBoundaryReplacer = strings.NewReplacer(". ", ".\n", "! ", "!\n", "? ", "?\n", "。 ", "。\n")
)

// splitObservationSentences breaks free-text observations into sentence-sized bullets.
func splitObservationSentences(s string) []string {
	s = strings.TrimSpace(newlineNormalizer.Replace(s))
	if s == "" {
		return nil
	}
	var out []string
	for _, block := range strings.Split(s, "\n") {
		if block = strings.TrimSpace(block); block == "" {
			continue
		}
		if endsWithAny(block, ".", "!", "?", "。") {
			block += "\n"
		}
		for _, part := range strings.Split(sentenceBoundaryReplacer.Replace(block), "\n") {
			out = appendTrimmed(out, part)
		}
	}
	return out
}

func appendTrimmed(dst []string, s string) []string {
	if s = strings.TrimSpace(s); s != "" {
		return append(dst, s)
	}
	return dst
}

func endsWithAny(s string, suffixes ...string) bool {
	for _, suf := range suffixes {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}

func normLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
