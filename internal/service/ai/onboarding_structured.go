package ai

import (
	"strings"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/dto"
)

const (
	SeverityMild     = "mild"
	SeverityModerate = "moderate"
	SeverityDense    = "dense"

	PhaseCalmFirst    = "calm_first"
	PhaseCanAddActive = "can_add_active"
)

var allowedPrimaryRegions = map[string]struct{}{
	"cheeks": {}, "t_zone": {}, "forehead": {}, "nose": {}, "chin": {},
	"jaw": {}, "perioral": {}, "temples": {},
}

var allowedConcernTypes = map[string]struct{}{
	"inflammatory_acne": {}, "comedones": {}, "pih": {}, "redness_irritation": {},
	"wrinkles": {}, "dry_lips": {}, "oiliness": {}, "dryness": {},
	"large_pores": {}, "uneven_tone": {}, "texture": {},
}

// regionAliases maps Vietnamese / casual labels → stable region ids.
var regionAliases = map[string]string{
	"ma": "cheeks", "má": "cheeks", "hai ma": "cheeks", "hai má": "cheeks", "cheeks": "cheeks",
	"t-zone": "t_zone", "t zone": "t_zone", "t_zone": "t_zone",
	"tran-mui-cam": "t_zone", "trán–mũi–cằm": "t_zone", "trán-mũi-cằm": "t_zone",
	"tran": "forehead", "trán": "forehead", "forehead": "forehead",
	"mui": "nose", "mũi": "nose", "nose": "nose",
	"cam": "chin", "cằm": "chin", "chin": "chin",
	"ham": "jaw", "hàm": "jaw", "jaw": "jaw", "jawline": "jaw",
	"quanh mieng": "perioral", "quanh miệng": "perioral", "perioral": "perioral",
	"thai duong": "temples", "thái dương": "temples", "temples": "temples",
}

var concernTypeAliases = map[string]string{
	"mun viem": "inflammatory_acne", "mụn viêm": "inflammatory_acne",
	"inflammatory_acne": "inflammatory_acne", "inflammatory acne": "inflammatory_acne",
	"mun an": "comedones", "mụn ẩn": "comedones", "mun an duoi da": "comedones",
	"closed comedones": "comedones", "closed_comedones": "comedones", "closed comedone": "comedones",
	"mun coi": "comedones", "mụn cồi": "comedones", "comedones": "comedones",
	"whiteheads": "comedones", "blackheads": "comedones", "dau den": "comedones", "đầu đen": "comedones",
	"tham": "pih", "thâm": "pih", "pih": "pih", "post acne marks": "pih",
	"do kich": "redness_irritation", "đỏ–kích": "redness_irritation",
	"do-kich": "redness_irritation", "redness_irritation": "redness_irritation",
	"redness": "redness_irritation", "kich ung": "redness_irritation",
	"nep": "wrinkles", "nếp": "wrinkles", "wrinkles": "wrinkles",
	"kho moi": "dry_lips", "khô môi": "dry_lips", "dry_lips": "dry_lips",
	"dau": "oiliness", "dầu": "oiliness", "oiliness": "oiliness",
	"kho": "dryness", "khô": "dryness", "dryness": "dryness",
	"lo chan long": "large_pores", "lỗ chân lông": "large_pores", "large_pores": "large_pores",
	"khong deu mau": "uneven_tone", "không đều màu": "uneven_tone", "uneven_tone": "uneven_tone",
	"san": "texture", "sần": "texture", "texture": "texture",
}

func normalizeSeverityLevel(raw string, obs dto.OnboardingSkinObservations) string {
	switch normLower(raw) {
	case SeverityMild, SeverityModerate, SeverityDense:
		return normLower(raw)
	}
	return deriveSeverityFromObservations(obs)
}

func deriveSeverityFromObservations(obs dto.OnboardingSkinObservations) string {
	acne := normLower(obs.AcneStatus)
	red := normLower(obs.Redness)
	if acne == "cystic_acne" || red == "severe" {
		return SeverityDense
	}
	if acne == "inflammatory_acne" && (red == "moderate" || red == "severe") {
		return SeverityDense
	}
	if acne == "inflammatory_acne" || red == "moderate" {
		return SeverityModerate
	}
	if acne == "few_whiteheads" || red == "mild" {
		return SeverityMild
	}
	return SeverityMild
}

func normalizePhase(raw string, severity string, obs dto.OnboardingSkinObservations) string {
	derived := derivePhase(severity, obs)
	switch normLower(raw) {
	case PhaseCalmFirst:
		return PhaseCalmFirst
	case PhaseCanAddActive:
		// Never let the model override a safer derived calm_first.
		if derived == PhaseCalmFirst {
			return PhaseCalmFirst
		}
		return PhaseCanAddActive
	}
	return derived
}

func derivePhase(severity string, obs dto.OnboardingSkinObservations) string {
	if severity == SeverityDense || severity == SeverityModerate {
		acne := normLower(obs.AcneStatus)
		if acne == "inflammatory_acne" || acne == "cystic_acne" {
			return PhaseCalmFirst
		}
	}
	acne := normLower(obs.AcneStatus)
	red := normLower(obs.Redness)
	if acne == "cystic_acne" || red == "severe" || red == "moderate" {
		return PhaseCalmFirst
	}
	if barrierLike(obs) {
		return PhaseCalmFirst
	}
	return PhaseCanAddActive
}

func barrierLike(obs dto.OnboardingSkinObservations) bool {
	if normLower(obs.OverallSkinType) == "sensitive" {
		return true
	}
	red := normLower(obs.Redness)
	return red == "moderate" || red == "severe"
}

func normalizePrimaryRegions(raw []string, obs dto.OnboardingSkinObservations, detailed string) []string {
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := allowedPrimaryRegions[id]; !ok {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, r := range raw {
		add(mapRegionLabel(r))
	}
	if len(out) == 0 {
		for _, id := range inferRegionsFromText(detailed + " " + obs.AcneStatus) {
			add(id)
		}
	}
	if len(out) == 0 {
		// Fallback from structured cues.
		if obs.Cheeks == "dry" || obs.AcneStatus != "" && obs.AcneStatus != "clear" {
			add("cheeks")
		}
		if normLower(obs.TZone) == "very_oily" || normLower(obs.OilinessLevel) == "high" || normLower(obs.OilinessLevel) == "very_high" {
			add("t_zone")
		}
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

func mapRegionLabel(label string) string {
	n := normalizeConcernLabel(label)
	if id, ok := regionAliases[n]; ok {
		return id
	}
	raw := normLower(label)
	for k, id := range regionAliases {
		if strings.Contains(raw, k) {
			return id
		}
	}
	if _, ok := allowedPrimaryRegions[raw]; ok {
		return raw
	}
	return ""
}

func inferRegionsFromText(text string) []string {
	raw := strings.ToLower(text)
	var out []string
	checks := []struct {
		id   string
		keys []string
	}{
		{"cheeks", []string{"má", "ma ", "cheek"}},
		{"forehead", []string{"trán", "tran", "forehead"}},
		{"nose", []string{"mũi", "mui", "nose"}},
		{"chin", []string{"cằm", "cam ", "chin"}},
		{"jaw", []string{"hàm", "ham ", "jaw"}},
		{"perioral", []string{"quanh miệng", "quanh mieng", "perioral", "mép"}},
		{"t_zone", []string{"trán–mũi–cằm", "tran-mui-cam", "t-zone", "t zone"}},
	}
	for _, c := range checks {
		for _, k := range c.keys {
			if strings.Contains(raw, k) {
				out = append(out, c.id)
				break
			}
		}
	}
	return out
}

func normalizeConcernTypes(raw []string, main []string, obs dto.OnboardingSkinObservations) []string {
	out := make([]string, 0, 6)
	seen := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := allowedConcernTypes[id]; !ok {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, r := range raw {
		add(mapConcernTypeLabel(r))
	}
	for _, r := range main {
		add(mapConcernTypeLabel(r))
	}
	inferConcernTypesFromObservations(add, obs)
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func mapConcernTypeLabel(label string) string {
	n := normalizeConcernLabel(label)
	if id, ok := concernTypeAliases[n]; ok {
		return id
	}
	raw := normLower(label)
	// Prefer longer / more specific keys; short keys need whole-token match
	// so "khỏe" ≠ dryness and "đầu" ≠ oiliness.
	type scored struct {
		key string
		id  string
	}
	var hits []scored
	for k, id := range concernTypeAliases {
		if concernAliasKeyMatches(raw, k) || concernAliasKeyMatches(n, k) {
			hits = append(hits, scored{k, id})
		}
	}
	if len(hits) > 0 {
		best := hits[0]
		for _, h := range hits[1:] {
			if len([]rune(h.key)) > len([]rune(best.key)) {
				best = h
			}
		}
		return best.id
	}
	// Map stable profile concerns → concern_types
	switch mapOnboardingConcernLabel(label) {
	case "acne":
		// Safety: closed-comedone wording must not become inflammatory_acne.
		if strings.Contains(raw, "ẩn") || strings.Contains(raw, "cồi") || strings.Contains(raw, "coi") ||
			strings.Contains(raw, "whitehead") || strings.Contains(raw, "blackhead") ||
			strings.Contains(raw, "closed comedone") || strings.Contains(n, "mun an") {
			return "comedones"
		}
		return "inflammatory_acne"
	case "hyperpigmentation":
		return "pih"
	case "redness", "weak_barrier":
		return "redness_irritation"
	case "dryness", "dehydration":
		return "dryness"
	case "large_pores":
		return "large_pores"
	case "uneven_texture":
		return "texture"
	}
	return ""
}

// concernAliasKeyMatches maps alias keys against a label.
// Keys ≤4 runes must match a whole whitespace-separated token (avoids "kho"∈"khỏe").
func concernAliasKeyMatches(label, key string) bool {
	key = strings.TrimSpace(key)
	label = strings.TrimSpace(label)
	if key == "" || label == "" {
		return false
	}
	if label == key {
		return true
	}
	if utf8.RuneCountInString(key) <= 4 {
		for _, tok := range strings.Fields(label) {
			if tok == key {
				return true
			}
		}
		return false
	}
	return strings.Contains(label, key)
}

func inferConcernTypesFromObservations(add func(string), obs dto.OnboardingSkinObservations) {
	switch normLower(obs.AcneStatus) {
	case "inflammatory_acne", "cystic_acne":
		add("inflammatory_acne")
	case "few_whiteheads":
		add("comedones")
	}
	switch normLower(obs.Pigmentation) {
	case "hyperpigmentation", "dark_spots", "slight_uneven":
		add("pih")
		add("uneven_tone")
	}
	switch normLower(obs.Redness) {
	case "mild", "moderate", "severe":
		add("redness_irritation")
	}
	if obsLargePoreSizes.has(obs.PoreSize) {
		add("large_pores")
	}
	if normLower(obs.OverallSkinType) == "dry" {
		add("dryness")
	}
	if obsHighOilinessLevels.has(obs.OilinessLevel) {
		add("oiliness")
	}
	if obsUnevenTextures.has(obs.Texture) {
		add("texture")
	}
}

func normalizeSummary(raw, detailed string, severity string, locale string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		s = firstSentences(detailed, 2)
	}
	s = scrubForbiddenSummaryClaims(s, severity)
	if s == "" {
		if strings.EqualFold(locale, "en") {
			switch severity {
			case SeverityDense:
				return "Photos show a dense, inflamed cluster — calm and soothe first, no strong actives yet."
			case SeverityModerate:
				return "Photos show clear inflamed spots in a few areas — gentle care first."
			default:
				return "Photos show mild cues — a simple base routine is enough to start."
			}
		}
		switch severity {
		case SeverityDense:
			return "Trên ảnh thấy cụm mụn viêm dày / đỏ rõ — ưu tiên làm dịu trước, chưa đẩy hoạt chất mạnh."
		case SeverityModerate:
			return "Trên ảnh thấy mụn viêm rõ ở vài vùng — chăm nhẹ, ổn định trước."
		default:
			return "Trên ảnh dấu hiệu nhẹ — bắt đầu với nền rửa dịu + dưỡng + kem chống nắng là đủ."
		}
	}
	return s
}

func firstSentences(text string, n int) string {
	parts := splitObservationSentences(text)
	if len(parts) == 0 {
		return strings.TrimSpace(text)
	}
	if len(parts) > n {
		parts = parts[:n]
	}
	return strings.Join(parts, " ")
}

func scrubForbiddenSummaryClaims(s, severity string) string {
	lower := strings.ToLower(s)
	// Ban downplaying dense flares.
	if severity == SeverityDense || severity == SeverityModerate {
		bannedDownplay := []string{
			"không quá nặng", "khong qua nang", "not too bad", "not severe",
			"chỉ nhẹ", "chi nhe", "only mild", "nothing serious",
		}
		for _, b := range bannedDownplay {
			if strings.Contains(lower, b) {
				// Drop the whole soft summary — caller will fall back.
				return ""
			}
		}
	}
	// Ban timeline promises.
	bannedPromise := []string{
		"2–3 tuần", "2-3 tuần", "2–3 tuan", "2-3 tuan",
		"2-3 weeks", "2–3 weeks", "cải thiện rõ", "cai thien ro",
		"clear improvement", "hết mụn", "het mun", "clear in a week",
		"7 ngày", "7 ngay", "in 7 days",
	}
	for _, b := range bannedPromise {
		if strings.Contains(lower, b) {
			return ""
		}
	}
	return strings.TrimSpace(s)
}

// applyStructuredOnboardingFields normalizes vision structured readback fields onto the response.
func applyStructuredOnboardingFields(out *dto.OnboardingSkinAnalyzeResponse, rawSeverity string, rawRegions, rawConcernTypes []string, rawPhase, rawSummary, locale string) {
	if out == nil {
		return
	}
	obs := dto.OnboardingSkinObservations{}
	if out.SkinObservations != nil {
		obs = *out.SkinObservations
	}
	out.SeverityLevel = normalizeSeverityLevel(rawSeverity, obs)
	out.PrimaryRegions = normalizePrimaryRegions(rawRegions, obs, out.DetailedObservations)
	out.ConcernTypes = normalizeConcernTypes(rawConcernTypes, out.MainConcerns, obs)
	out.Phase = normalizePhase(rawPhase, out.SeverityLevel, obs)
	out.Summary = normalizeSummary(rawSummary, out.DetailedObservations, out.SeverityLevel, locale)
}
