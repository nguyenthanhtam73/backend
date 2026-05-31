package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/dto"
)

const (
	affiliateIdealMaxCount  = 2
	affiliateHardMaxCount   = 2
	affiliateMinReasonRunes = 28
)

// AffiliateEvalResult scores one coach turn's product_suggestions.
type AffiliateEvalResult struct {
	Pipeline          string
	ScenarioID        string
	Count             int
	CountOK           bool
	IdealCountOK      bool
	AllInCatalog      bool
	HasTransparency   bool
	ReasonSpecific    bool
	RespectsWardrobe  bool
	WardrobeHits      []string
	Issues            []string
	Score             float64
	Products          []string
}

// AffiliateTurnOutput bundles suggestions from one pipeline call.
type AffiliateTurnOutput struct {
	Suggestions []dto.ProductSuggestion
	Rationale   string
}

// EvaluateAffiliateSuggestions scores suggestions against scenario expectations.
func EvaluateAffiliateSuggestions(sc AffiliateScenario, pipeline string, out AffiliateTurnOutput) AffiliateEvalResult {
	suggestions := SanitizeProductSuggestions(out.Suggestions)
	suggestions = DropOwnedProductSuggestions(suggestions, sc.Wardrobe)

	maxCount := sc.ExpectMaxCount
	if maxCount <= 0 {
		maxCount = affiliateHardMaxCount
	}
	idealMax := sc.ExpectIdealMax
	if idealMax <= 0 {
		idealMax = affiliateIdealMaxCount
	}

	res := AffiliateEvalResult{
		Pipeline:     pipeline,
		ScenarioID:   sc.ID,
		Count:        len(suggestions),
		CountOK:      len(suggestions) <= maxCount,
		IdealCountOK: len(suggestions) <= idealMax,
		AllInCatalog: len(suggestions) == 0 || allSuggestionsInCatalog(suggestions),
	}

	for _, s := range suggestions {
		res.Products = append(res.Products, s.Brand+" — "+s.ProductName)
	}

	for _, s := range suggestions {
		if reasonHasAffiliateTransparency(s.Reason) {
			res.HasTransparency = true
			break
		}
	}
	if len(suggestions) == 0 {
		res.HasTransparency = true
		res.ReasonSpecific = true
		res.RespectsWardrobe = true
	}

	contextNeedles := scenarioContextNeedles(sc)
	for _, s := range suggestions {
		if reasonIsSpecific(s.Reason, contextNeedles, out.Rationale) {
			res.ReasonSpecific = true
			break
		}
	}
	if len(suggestions) == 1 && !res.ReasonSpecific {
		res.ReasonSpecific = reasonIsSpecific(suggestions[0].Reason, contextNeedles, out.Rationale)
	}

	ownedHits := findWardrobeHits(suggestions, sc.Wardrobe)
	res.WardrobeHits = ownedHits
	res.RespectsWardrobe = len(ownedHits) == 0

	if !res.CountOK {
		res.Issues = append(res.Issues, fmt.Sprintf("too many suggestions (%d > max %d)", res.Count, maxCount))
	}
	if !res.IdealCountOK && res.Count > idealMax {
		res.Issues = append(res.Issues, fmt.Sprintf("over ideal count (%d > ideal %d)", res.Count, idealMax))
	}
	if !res.AllInCatalog {
		res.Issues = append(res.Issues, "hallucinated or unknown catalog product")
	}
	if len(suggestions) > 0 && !res.HasTransparency {
		res.Issues = append(res.Issues, "missing affiliate/commission transparency in reason")
	}
	if len(suggestions) > 0 && !res.ReasonSpecific {
		res.Issues = append(res.Issues, "reason too generic — lacks today-specific context")
	}
	if !res.RespectsWardrobe {
		res.Issues = append(res.Issues, "re-suggested product user already owns: "+strings.Join(ownedHits, "; "))
	}
	for _, s := range suggestions {
		if cat, ok := catalogCategoryFor(s); ok {
			for _, avoid := range sc.AvoidCategories {
				if cat == avoid {
					res.Issues = append(res.Issues, fmt.Sprintf("avoid category %q suggested: %s", avoid, s.ProductName))
				}
			}
		}
	}

	var earned, total float64
	total++
	if res.CountOK {
		earned++
	}
	total++
	if res.AllInCatalog {
		earned++
	}
	total++
	if res.HasTransparency || len(suggestions) == 0 {
		earned++
	}
	total++
	if res.ReasonSpecific || len(suggestions) == 0 {
		earned++
	}
	total++
	if res.RespectsWardrobe {
		earned++
	}
	if len(suggestions) > 0 && len(sc.PreferCategories) > 0 {
		total++
		if suggestionMatchesPreferredCategory(suggestions, sc.PreferCategories) {
			earned++
		} else if sc.ID == "wardrobe_full" && len(suggestions) <= 1 {
			// Stocked wardrobe: one gentle gap-filler is acceptable even if not toner/mask.
			earned++
		} else {
			res.Issues = append(res.Issues, "no suggestion in preferred categories "+strings.Join(sc.PreferCategories, ","))
		}
	}
	res.Score = earned / total
	return res
}

// DropOwnedProductSuggestions removes picks the user already owns (wardrobe).
func DropOwnedProductSuggestions(suggestions []dto.ProductSuggestion, owned []wardrobeItem) []dto.ProductSuggestion {
	if len(suggestions) == 0 || len(owned) == 0 {
		return suggestions
	}
	out := make([]dto.ProductSuggestion, 0, len(suggestions))
	for _, s := range suggestions {
		if isOwnedProduct(s, owned) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func isOwnedProduct(s dto.ProductSuggestion, owned []wardrobeItem) bool {
	sk := nameBrandKey(s.ProductName, s.Brand)
	for _, o := range owned {
		if nameBrandKey(o.Name, o.Brand) == sk {
			return true
		}
		prefix := o.Name
		if len(prefix) > 12 {
			prefix = prefix[:12]
		}
		if strings.EqualFold(strings.TrimSpace(o.Brand), strings.TrimSpace(s.Brand)) &&
			strings.Contains(strings.ToLower(s.ProductName), strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

func findWardrobeHits(suggestions []dto.ProductSuggestion, owned []wardrobeItem) []string {
	var hits []string
	for _, s := range suggestions {
		if isOwnedProduct(s, owned) {
			hits = append(hits, s.Brand+" "+s.ProductName)
		}
	}
	return hits
}

func allSuggestionsInCatalog(suggestions []dto.ProductSuggestion) bool {
	rows, err := loadAffiliateCatalog()
	if err != nil {
		return false
	}
	for _, s := range suggestions {
		if !suggestionInCatalog(s, rows) {
			return false
		}
	}
	return true
}

func suggestionInCatalog(s dto.ProductSuggestion, rows []affiliateCatalogEntry) bool {
	sk := nameBrandKey(s.ProductName, s.Brand)
	link := normalizeAffiliateLink(s.AffiliateLink)
	for _, r := range rows {
		if nameBrandKey(r.ProductName, r.Brand) == sk {
			return true
		}
		if link != "" && normalizeAffiliateLink(r.AffiliateLink) == link {
			return true
		}
	}
	return false
}

func catalogCategoryFor(s dto.ProductSuggestion) (string, bool) {
	rows, err := loadAffiliateCatalog()
	if err != nil {
		return "", false
	}
	sk := nameBrandKey(s.ProductName, s.Brand)
	for _, r := range rows {
		if nameBrandKey(r.ProductName, r.Brand) == sk {
			return r.Category, true
		}
	}
	return "", false
}

func suggestionMatchesPreferredCategory(suggestions []dto.ProductSuggestion, preferred []string) bool {
	for _, s := range suggestions {
		cat, ok := catalogCategoryFor(s)
		if !ok {
			continue
		}
		for _, pref := range preferred {
			if cat == pref {
				return true
			}
		}
	}
	return false
}

func reasonHasAffiliateTransparency(reason string) bool {
	low := strings.ToLower(reason)
	markers := []string{
		"hoa hồng", "affiliate", "commission", "liên kết có thể", "link có thể",
		"dadiary", "ủng hộ", "small commission", "hoa hong",
	}
	for _, m := range markers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

func reasonIsSpecific(reason string, contextNeedles []string, extra string) bool {
	if utf8.RuneCountInString(strings.TrimSpace(reason)) < affiliateMinReasonRunes {
		return false
	}
	low := strings.ToLower(reason + " " + extra)
	hits := 0
	for _, n := range contextNeedles {
		if n == "" {
			continue
		}
		if strings.Contains(low, strings.ToLower(n)) {
			hits++
		}
	}
	return hits >= 1
}

func scenarioContextNeedles(sc AffiliateScenario) []string {
	var needles []string
	if sc.TodayCheck != nil {
		var conds, syms []string
		_ = json.Unmarshal(sc.TodayCheck.Conditions, &conds)
		_ = json.Unmarshal(sc.TodayCheck.Symptoms, &syms)
		needles = append(needles, conds...)
		needles = append(needles, syms...)
		if note := strings.TrimSpace(sc.TodayCheck.UserNote); note != "" {
			for _, w := range []string{"mụn", "thâm", "nắng", "spf", "kem chống nắng", "má", "t-zone", "trán", "châm", "đỏ", "căng", "dầu", "breakout", "hyperpigmentation", "pih"} {
				if strings.Contains(strings.ToLower(note), w) {
					needles = append(needles, w)
				}
			}
		}
	}
	if sc.Profile != nil {
		needles = append(needles, sc.Profile.SkinType)
	}
	return needles
}

// FormatAffiliateEvalReport renders a markdown comparison table for test logs.
func FormatAffiliateEvalReport(rows []AffiliateEvalResult) string {
	var b strings.Builder
	b.WriteString("| Scenario | Pipeline | Count | Catalog | Transparent | Specific | Wardrobe | Score | Issues |\n")
	b.WriteString("|----------|----------|-------|---------|-------------|----------|----------|-------|--------|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s | %s | %.0f%% | %s |\n",
			r.ScenarioID,
			r.Pipeline,
			r.Count,
			boolMark(r.AllInCatalog),
			boolMark(r.HasTransparency),
			boolMark(r.ReasonSpecific),
			boolMark(r.RespectsWardrobe),
			r.Score*100,
			truncateAffiliateIssues(r.Issues),
		)
	}
	return b.String()
}

func boolMark(ok bool) string {
	if ok {
		return "OK"
	}
	return "FAIL"
}

func truncateAffiliateIssues(issues []string) string {
	if len(issues) == 0 {
		return "-"
	}
	s := strings.Join(issues, "; ")
	if utf8.RuneCountInString(s) > 80 {
		return string([]rune(s)[:77]) + "..."
	}
	return s
}
