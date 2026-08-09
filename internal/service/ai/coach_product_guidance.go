package ai

import (
	"regexp"
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

var userInterfaceLocaleRe = regexp.MustCompile(`(?i)USER_INTERFACE_LOCALE:\s*(en|vi)\b`)

// Word-ish boundaries so "steady" does not match "unsteady".
func containsToken(haystack, token string) bool {
	token = strings.TrimSpace(strings.ToLower(token))
	if token == "" {
		return false
	}
	haystack = strings.ToLower(haystack)
	idx := 0
	for {
		i := strings.Index(haystack[idx:], token)
		if i < 0 {
			return false
		}
		i += idx
		beforeOK := i == 0 || !isTokenChar(haystack[i-1])
		after := i + len(token)
		afterOK := after >= len(haystack) || !isTokenChar(haystack[after])
		if beforeOK && afterOK {
			return true
		}
		idx = i + 1
	}
}

func isTokenChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}

// localeFromUserContext reads USER_INTERFACE_LOCALE from coach context (en|vi).
func localeFromUserContext(userContext string) string {
	m := userInterfaceLocaleRe.FindStringSubmatch(userContext)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(m[1]))
}

// InferCarePhaseFromUserContext guesses calm_first vs can_add_active from USER_MEMORY / coach context.
// Uses specific multi-word / clinical tokens — avoids short substrings like "do ", "barrier", "dense".
// Flare evidence wins over vague steady words (mild/steady); only explicit can_add_active overrides.
func InferCarePhaseFromUserContext(userContext string) string {
	low := strings.ToLower(userContext)

	if strings.Contains(low, "can_add_active") {
		// Explicit phase tag wins only when no strong flare clinical tokens.
		if !hasStrongFlare(low) {
			return PhaseCanAddActive
		}
		return PhaseCalmFirst
	}

	if hasStrongFlare(low) {
		return PhaseCalmFirst
	}

	// Acute symptoms: one hit is enough.
	acuteHints := []string{
		"flare", "stinging", "burning", "irritated", "irritation",
		"đỏ rát", "do rat", "sưng đỏ", "sung do",
		"châm chích", "cham chich", "nóng rát", "nong rat",
	}
	// Soft tags often live in profile forever — need ≥2, or also appear in today/vision.
	softHints := []string{"redness", "weak_barrier", "barrier_signal"}

	acute := countHintHits(low, acuteHints)
	soft := countHintHits(low, softHints)
	if acute >= 1 {
		return PhaseCalmFirst
	}
	if soft >= 2 {
		return PhaseCalmFirst
	}
	if soft >= 1 && softFlareInTodayOrVision(low, softHints) {
		return PhaseCalmFirst
	}

	strongSteady := []string{"ổn định", "on dinh", "few_whiteheads"}
	for _, h := range strongSteady {
		if strings.Contains(low, h) {
			return PhaseCanAddActive
		}
	}
	return PhaseCanAddActive
}

func countHintHits(low string, hints []string) int {
	hits := 0
	for _, h := range hints {
		if strings.Contains(h, " ") || strings.ContainsAny(h, "áàảãạăắằẳẵặâấầẩẫậéèẻẽẹêếềểễệíìỉĩịóòỏõọôốồổỗộơớờởỡợúùủũụưứừửữựýỳỷỹỵđ") {
			if strings.Contains(low, h) {
				hits++
			}
			continue
		}
		if containsToken(low, h) {
			hits++
		}
	}
	return hits
}

func softFlareInTodayOrVision(low string, softHints []string) bool {
	for _, marker := range []string{"vision_obs:", "today_check_in", "today_check-in", "today's check", "today check-in"} {
		section := sectionAfterMarker(low, marker)
		if section == "" {
			continue
		}
		if countHintHits(section, softHints) >= 1 {
			return true
		}
	}
	return false
}

// sectionAfterMarker returns text after marker until the next major context header
// so profile/memory tags after TODAY_CHECK_IN are not treated as "today".
func sectionAfterMarker(low, marker string) string {
	i := strings.Index(low, marker)
	if i < 0 {
		return ""
	}
	rest := low[i+len(marker):]
	end := len(rest)
	for _, cut := range []string{
		"\n## ",
		"\nuser_memory",
		"\nrecent_diary",
		"\nskin_profile_context",
		"\nvision_obs:",
		"\ntoday_check_in",
		"\ntoday_check-in",
	} {
		j := strings.Index(rest, cut)
		if j >= 0 && j < end {
			end = j
		}
	}
	return rest[:end]
}

func hasStrongFlare(low string) bool {
	strongFlare := []string{
		"calm_first",
		"possibly_compromised",
		"compromised barrier",
		"barrier compromised",
		"inflammatory_acne",
		"cystic",
		"viêm dày",
		"viem day",
		"mụn viêm",
		"mun viem",
		"dense inflammatory",
		"severity_dense",
		"severity: dense",
		"severity dense",
		"severity_level\": \"dense",
		"severity_level\":\"dense",
	}
	for _, h := range strongFlare {
		if strings.Contains(low, h) {
			return true
		}
	}
	return false
}

// ProductSuggestionsToGuidance maps sanitized suggestions into onboarding-shaped guidance items.
// NameOrCategory stays a generic role label; brand/product live only on commerce fields.
func ProductSuggestionsToGuidance(suggestions []dto.ProductSuggestion, phase, locale string) []dto.ProductGuidanceItem {
	if len(suggestions) == 0 {
		return nil
	}
	if phase != PhaseCalmFirst && phase != PhaseCanAddActive {
		phase = PhaseCanAddActive
	}
	out := make([]dto.ProductGuidanceItem, 0, len(suggestions))
	for _, s := range suggestions {
		step := strings.TrimSpace(s.Step)
		if step == "" {
			step = "soothe"
		}
		item := dto.ProductGuidanceItem{
			Step:               step,
			Phase:              phase,
			Category:           step,
			NameOrCategory:     genericRoleLabel(step, step, locale),
			Why:                strings.TrimSpace(s.Reason),
			Benefits:           shortBenefits(s.Benefits, 2),
			HowToUse:           strings.TrimSpace(s.HowToUse),
			Caution:            strings.TrimSpace(s.Caution),
			AffiliateProductID: strings.TrimSpace(s.ProductID),
			ProductName:        s.ProductName,
			Brand:              s.Brand,
			AffiliateLink:      s.AffiliateLink,
			PriceRange:         s.PriceRange,
		}
		out = append(out, item)
	}
	return out
}

func shortBenefits(in []string, n int) []string {
	if len(in) == 0 || n <= 0 {
		return nil
	}
	if len(in) <= n {
		return append([]string(nil), in...)
	}
	return append([]string(nil), in[:n]...)
}

// FinalizeCoachCommerce applies sanitize, phase filter, and builds product_guidance.
// phaseExtra is appended only for phase inference (e.g. vision observations) — not for wardrobe parsing.
func FinalizeCoachCommerce(
	suggestions []dto.ProductSuggestion,
	userContext string,
	locale string,
	phaseExtra string,
) (picks []dto.ProductSuggestion, guidance []dto.ProductGuidanceItem, phase string) {
	phaseSrc := userContext
	if x := strings.TrimSpace(phaseExtra); x != "" {
		phaseSrc = userContext + "\nVISION_OBS:\n" + x
	}
	phase = InferCarePhaseFromUserContext(phaseSrc)
	picks = FinalizeProductSuggestionsLocale(suggestions, userContext, locale)
	picks = FilterProductSuggestionsForPhase(picks, phase)
	for i := range picks {
		picks[i].Phase = phase
	}
	guidance = ProductSuggestionsToGuidance(picks, phase, locale)
	return picks, guidance, phase
}
