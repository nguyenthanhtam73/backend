package ai

import (
	"encoding/json"
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

const starterAffiliatePickMax = 1

// OwnedWardrobeItem is a minimal owned-product row for affiliate deduping.
type OwnedWardrobeItem struct {
	Name     string
	Brand    string
	Category string
}

func ownedToWardrobeItems(owned []OwnedWardrobeItem) []wardrobeItem {
	if len(owned) == 0 {
		return nil
	}
	out := make([]wardrobeItem, 0, len(owned))
	for _, o := range owned {
		out = append(out, wardrobeItem{
			Name:     o.Name,
			Brand:    o.Brand,
			Category: o.Category,
		})
	}
	return out
}

// PickStarterAffiliateSuggestions chooses up to one catalog product from onboarding
// answers when the LLM returned none (common on coach-welcome).
func PickStarterAffiliateSuggestions(onboardingJSON []byte, locale string) []dto.ProductSuggestion {
	rows, err := loadAffiliateCatalog()
	if err != nil || len(rows) == 0 {
		return nil
	}

	var snap struct {
		SkinType     string   `json:"skin_type"`
		Goal         string   `json:"goal"`
		Contexts     []string `json:"contexts"`
		BodyConcerns []string `json:"body_concerns"`
	}
	_ = json.Unmarshal(onboardingJSON, &snap)

	skin := normalizeOnboardingSkinType(snap.SkinType)
	needles := onboardingConcernNeedles(snap.Goal, snap.BodyConcerns)
	wantsSPF := onboardingWantsSPF(snap.Contexts)

	bestScore := -1
	var best *affiliateCatalogEntry
	for i := range rows {
		r := &rows[i]
		score := scoreStarterCatalogEntry(*r, skin, needles, wantsSPF)
		if score > bestScore {
			bestScore = score
			best = r
		}
	}
	if best == nil || bestScore < 3 {
		best = defaultStarterCatalogEntry(rows, skin, wantsSPF)
	}
	if best == nil {
		return nil
	}

	reason := starterPickReason(locale, snap.Goal, skin, *best)
	if utf8RuneCount(reason) < affiliateMinReasonRunes {
		reason = starterPickReason(locale, "", skin, *best)
	}

	return []dto.ProductSuggestion{{
		ProductName:   best.ProductName,
		Brand:         best.Brand,
		Reason:        reason,
		AffiliateLink: best.AffiliateLink,
		PriceRange:    best.PriceRange,
		Priority:      "high",
	}}
}

// EnrichOnboardingSnapshotStarterAffiliate injects catalog picks into starter_routine when missing.
func EnrichOnboardingSnapshotStarterAffiliate(snapshot []byte, locale string, owned []OwnedWardrobeItem) []byte {
	if len(snapshot) == 0 {
		return snapshot
	}
	var snap map[string]any
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		return snapshot
	}
	srRaw, ok := snap["starter_routine"]
	if !ok || srRaw == nil {
		return snapshot
	}
	srBytes, err := json.Marshal(srRaw)
	if err != nil {
		return snapshot
	}
	var sr StarterRoutine
	if err := json.Unmarshal(srBytes, &sr); err != nil {
		return snapshot
	}
	if len(sr.ProductSuggestions) > 0 {
		return snapshot
	}

	onboardingBytes, err := json.Marshal(snap)
	if err != nil {
		return snapshot
	}
	picks := PickStarterAffiliateSuggestions(onboardingBytes, locale)
	picks = DropOwnedProductSuggestions(picks, ownedToWardrobeItems(owned))
	if len(picks) == 0 {
		return snapshot
	}
	if len(picks) > starterAffiliatePickMax {
		picks = picks[:starterAffiliatePickMax]
	}
	sr.ProductSuggestions = picks
	snap["starter_routine"] = sr
	out, err := json.Marshal(snap)
	if err != nil {
		return snapshot
	}
	return out
}

// LocaleFromOnboardingSnapshot reads "locale" from the persisted onboarding snapshot ("vi" | "en").
func LocaleFromOnboardingSnapshot(snapshot []byte) string {
	var snap struct {
		Locale string `json:"locale"`
	}
	_ = json.Unmarshal(snapshot, &snap)
	if strings.EqualFold(strings.TrimSpace(snap.Locale), "en") {
		return "en"
	}
	return "vi"
}

func normalizeOnboardingSkinType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "combo", "combination":
		return "combination"
	case "dry", "oily", "normal", "sensitive":
		return strings.ToLower(strings.TrimSpace(raw))
	case "prefer_not", "":
		return "normal"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func onboardingConcernNeedles(goal string, bodyConcerns []string) []string {
	needles := make([]string, 0, 8)
	switch strings.ToLower(strings.TrimSpace(goal)) {
	case "clear_acne":
		needles = append(needles, "acne", "oily", "clogged_pores")
	case "barrier":
		needles = append(needles, "weak_barrier", "sensitive", "dry", "dehydrated", "redness")
	case "glow":
		needles = append(needles, "dull", "dehydrated")
	case "anti_aging":
		needles = append(needles, "hyperpigmentation", "dull")
	}
	for _, c := range bodyConcerns {
		c = strings.ToLower(strings.TrimSpace(c))
		if c != "" {
			needles = append(needles, c)
		}
	}
	return needles
}

func onboardingWantsSPF(contexts []string) bool {
	for _, ctx := range contexts {
		switch strings.ToLower(strings.TrimSpace(ctx)) {
		case "outdoor", "travel", "gym", "shift_work":
			return true
		}
	}
	return false
}

func scoreStarterCatalogEntry(r affiliateCatalogEntry, skin string, needles []string, wantsSPF bool) int {
	score := 0
	if wantsSPF && r.Category == "spf" {
		score += 10
	}
	for _, st := range r.SkinTypes {
		if strings.EqualFold(st, skin) {
			score += 4
			break
		}
	}
	for _, n := range needles {
		for _, c := range r.Concerns {
			if strings.EqualFold(c, n) || strings.Contains(strings.ToLower(c), strings.ToLower(n)) {
				score += 3
			}
		}
	}
	if !wantsSPF && r.Category == "cleanser" {
		score += 2
	}
	return score
}

func defaultStarterCatalogEntry(rows []affiliateCatalogEntry, skin string, wantsSPF bool) *affiliateCatalogEntry {
	if wantsSPF {
		for i := range rows {
			if rows[i].Category == "spf" {
				return &rows[i]
			}
		}
	}
	preferHydrating := skin == "dry" || skin == "sensitive"
	for i := range rows {
		if rows[i].Category != "cleanser" {
			continue
		}
		id := rows[i].ID
		if preferHydrating && id == "cerave-hydrating-cleanser" {
			return &rows[i]
		}
		if !preferHydrating && id == "cerave-foaming-cleanser" {
			return &rows[i]
		}
	}
	for i := range rows {
		if rows[i].Category == "cleanser" {
			return &rows[i]
		}
	}
	if len(rows) > 0 {
		return &rows[0]
	}
	return nil
}

func starterPickReason(locale, goal, skin string, entry affiliateCatalogEntry) string {
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		g := strings.TrimSpace(goal)
		if g == "" {
			g = "your skincare goals"
		} else {
			g = strings.ReplaceAll(g, "_", " ")
		}
		return "Based on " + g + " and " + skin + " skin, " + entry.Brand + " " + entry.ProductName +
			" is a gentle starter step for your new AM/PM routine. Affiliate link — DaDiary may earn a small commission."
	}
	goalVI := goalLabelVI(goal)
	skinVI := skinLabelVI(skin)
	return "Theo " + goalVI + " và loại da " + skinVI + ", " + entry.Brand + " — " + entry.ProductName +
		" có thể là món khởi đầu nhẹ cho routine sáng/tối của bạn. Link affiliate có thể giúp DaDiary duy trì app (hoa hồng nhỏ)."
}

func goalLabelVI(goal string) string {
	switch strings.ToLower(strings.TrimSpace(goal)) {
	case "clear_acne":
		return "mục tiêu giảm mụn"
	case "barrier":
		return "mục tiêu phục hồi lớp bảo vệ da"
	case "glow":
		return "mục tiêu da sáng khỏe"
	case "anti_aging":
		return "mục tiêu chống lão hóa nhẹ"
	default:
		return "hồ sơ làm quen da của bạn"
	}
}

func skinLabelVI(skin string) string {
	switch skin {
	case "oily":
		return "dầu"
	case "dry":
		return "khô"
	case "combination":
		return "hỗn hợp"
	case "sensitive":
		return "nhạy cảm"
	default:
		return "thường"
	}
}

func utf8RuneCount(s string) int {
	return len([]rune(s))
}
