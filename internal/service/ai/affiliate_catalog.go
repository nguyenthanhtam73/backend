package ai

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/dadiary/backend/internal/dto"
)

//go:embed affiliate_catalog.json
var affiliateCatalogJSON []byte

const maxProductSuggestions = 2

// affiliateCatalogEntry is the server-side source of truth for affiliate picks.
type affiliateCatalogEntry struct {
	ID              string   `json:"id"`
	ProductName     string   `json:"product_name"`
	Brand           string   `json:"brand"`
	Category        string   `json:"category"`
	Step            string   `json:"step,omitempty"` // cleanse|moisturize|spf|soothe|treat
	SkinTypes       []string `json:"skin_types"`
	Concerns        []string `json:"concerns"`
	Phases          []string `json:"phases,omitempty"` // calm_first | can_add_active
	ActiveKind      string   `json:"active_kind,omitempty"`
	ReasonTemplate  string   `json:"reason_template,omitempty"`
	PriceRange      string   `json:"price_range"`
	AffiliateLink   string   `json:"affiliate_link"`
}

var (
	catalogOnce sync.Once
	catalogRows []affiliateCatalogEntry
	catalogErr  error
)

func loadAffiliateCatalog() ([]affiliateCatalogEntry, error) {
	catalogOnce.Do(func() {
		if err := json.Unmarshal(affiliateCatalogJSON, &catalogRows); err != nil {
			catalogErr = err
			return
		}
	})
	return catalogRows, catalogErr
}

// ProductSuggestionsJSONField documents the coach JSON field appended to all AI output schemas.
const ProductSuggestionsJSONField = `
  "product_suggestions": [
    {
      "product_name": "<exact name from AFFILIATE_CATALOG>",
      "brand": "<exact brand from AFFILIATE_CATALOG>",
      "reason": "<1–2 sentences — WHY this product fits TODAY's skin signals; cite region/concern from context>",
      "affiliate_link": "<exact affiliate_link from AFFILIATE_CATALOG — never invent URLs>",
      "price_range": "<exact price_range from AFFILIATE_CATALOG>",
      "priority": "high|medium"
    }
    // 0–2 items (ideal 1). Use [] when nothing fits, wardrobe full, or severe irritation.
  ]`

// AffiliateRecommendationRulesBlock is shared coach guidance for picking catalog products.
const AffiliateRecommendationRulesBlock = `## Affiliate product picks (product_suggestions)
- Pick ONLY from AFFILIATE_CATALOG below — copy product_name, brand, affiliate_link, price_range exactly.
- Suggest **0–2 items max** (ideal **1**). Never stack 3+ products — feels salesy.
- Respect catalog **phases** + **step** + **active_kind**:
  · If skin is flaring / dense inflammation / barrier angry → phase calm_first: prefer cleanse/moisturize/spf/soothe; NEVER pick step=treat or active_kind bha|bp|aha.
  · If skin looks steadier → phase can_add_active: at most **one** treat (BHA or BP or patch) — do not stack actives.
- If USER_MEMORY has ## Wardrobe listing products the user already owns → **do NOT** re-recommend those; suggest only a clear gap (e.g. missing SPF) or return [].
- "reason" MUST be specific to TODAY (tags, photo cues, profile goal, wardrobe gap) — warm friend tone, not ad copy. You may adapt reason_template.
- "priority": "high" = directly fills today's top gap; "medium" = optional add-on. Treat/actives are medium unless clearly needed.
- Routine steps stay generic roles; product_suggestions is the ONLY branded slot.
- Skip affiliate picks when: stinging/redness flare, user 👎 affiliate picks often, wardrobe already complete, or no catalog item clearly fits.
- **Routine suggest:** if today's routine or tags show missing SPF / recent sun → product_suggestions SHOULD prioritize step/category "spf" from catalog (one pick max).
- Never invent products, brands, prices, or links. Empty array [] is valid and often best.`

// AppendAffiliateCoachContext injects catalog + rules into an AI user message.
func AppendAffiliateCoachContext(b *strings.Builder) {
	b.WriteString("\n\n")
	b.WriteString(AffiliateRecommendationRulesBlock)
	b.WriteString("\n\nAFFILIATE_CATALOG (authoritative — only source for product_suggestions):\n")
	b.WriteString(AffiliateCatalogPromptBlock())
}

// AffiliateCatalogPromptBlock returns a compact JSON array for prompt injection.
func AffiliateCatalogPromptBlock() string {
	rows, err := loadAffiliateCatalog()
	if err != nil || len(rows) == 0 {
		return "[]"
	}
	type promptRow struct {
		ID             string   `json:"id"`
		ProductName    string   `json:"product_name"`
		Brand          string   `json:"brand"`
		Category       string   `json:"category"`
		Step           string   `json:"step,omitempty"`
		SkinTypes      []string `json:"skin_types"`
		Concerns       []string `json:"concerns"`
		Phases         []string `json:"phases,omitempty"`
		ActiveKind     string   `json:"active_kind,omitempty"`
		ReasonTemplate string   `json:"reason_template,omitempty"`
		PriceRange     string   `json:"price_range"`
		AffiliateLink  string   `json:"affiliate_link"`
	}
	out := make([]promptRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, promptRow{
			ID:             r.ID,
			ProductName:    r.ProductName,
			Brand:          r.Brand,
			Category:       r.Category,
			Step:           r.Step,
			SkinTypes:      r.SkinTypes,
			Concerns:       r.Concerns,
			Phases:         r.Phases,
			ActiveKind:     r.ActiveKind,
			ReasonTemplate: r.ReasonTemplate,
			PriceRange:     r.PriceRange,
			AffiliateLink:  r.AffiliateLink,
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// SanitizeProductSuggestions validates AI output against the affiliate catalog.
// Unknown or hallucinated entries are dropped; links are always taken from catalog.
func SanitizeProductSuggestions(raw []dto.ProductSuggestion) []dto.ProductSuggestion {
	return SanitizeProductSuggestionsLocale(raw, "")
}

// SanitizeProductSuggestionsLocale is like SanitizeProductSuggestions but fills
// default benefits/how-to/caution in the requested locale (en|vi; empty → vi).
func SanitizeProductSuggestionsLocale(raw []dto.ProductSuggestion, locale string) []dto.ProductSuggestion {
	if len(raw) == 0 {
		return []dto.ProductSuggestion{}
	}
	rows, err := loadAffiliateCatalog()
	if err != nil || len(rows) == 0 {
		return []dto.ProductSuggestion{}
	}

	byLink := make(map[string]affiliateCatalogEntry, len(rows))
	byNameBrand := make(map[string]affiliateCatalogEntry, len(rows))
	for _, r := range rows {
		link := normalizeAffiliateLink(r.AffiliateLink)
		if link != "" {
			byLink[link] = r
		}
		key := nameBrandKey(r.ProductName, r.Brand)
		if key != "" {
			byNameBrand[key] = r
		}
	}

	seen := make(map[string]struct{})
	out := make([]dto.ProductSuggestion, 0, maxProductSuggestions)
	for _, s := range raw {
		if len(out) >= maxProductSuggestions {
			break
		}
		entry, ok := matchCatalogEntry(s, byLink, byNameBrand)
		if !ok {
			continue
		}
		if _, dup := seen[entry.ID]; dup {
			continue
		}
		seen[entry.ID] = struct{}{}

		reason := strings.TrimSpace(s.Reason)
		if reason == "" {
			reason = strings.TrimSpace(entry.ReasonTemplate)
		}
		if reason == "" {
			continue
		}
		priority := normalizePriority(s.Priority)
		if catalogStep(entry) == "treat" && priority == "high" {
			// Treat/actives default lower unless already medium — calm surfaces shouldn't over-push.
			priority = "medium"
		}
		item := dto.ProductSuggestion{
			ProductName:   entry.ProductName,
			Brand:         entry.Brand,
			Reason:        reason,
			AffiliateLink: entry.AffiliateLink,
			PriceRange:    entry.PriceRange,
			Priority:      priority,
			ProductID:     entry.ID,
			Step:          catalogStep(entry),
			Benefits:      append([]string(nil), s.Benefits...),
			HowToUse:      strings.TrimSpace(s.HowToUse),
			Caution:       strings.TrimSpace(s.Caution),
		}
		if len(item.Benefits) == 0 {
			item.Benefits = defaultBenefitsForStep(item.Step, locale)
		}
		if item.HowToUse == "" {
			item.HowToUse = defaultHowToUseForStep(item.Step, locale)
		}
		if item.Caution == "" && (normLower(entry.ActiveKind) == "bha" || normLower(entry.ActiveKind) == "bp" || normLower(entry.ActiveKind) == "aha") {
			item.Caution = defaultActiveCaution(locale)
		}
		out = append(out, item)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return priorityRank(out[i].Priority) > priorityRank(out[j].Priority)
	})
	return out
}

// FinalizeProductSuggestions sanitizes catalog picks and drops items the user already owns
// when a ## Wardrobe block is present in userContext (USER_MEMORY).
func FinalizeProductSuggestions(raw []dto.ProductSuggestion, userContext string) []dto.ProductSuggestion {
	return FinalizeProductSuggestionsLocale(raw, userContext, "")
}

// FinalizeProductSuggestionsLocale is FinalizeProductSuggestions with locale-aware defaults.
func FinalizeProductSuggestionsLocale(raw []dto.ProductSuggestion, userContext, locale string) []dto.ProductSuggestion {
	out := SanitizeProductSuggestionsLocale(raw, locale)
	if owned := parseWardrobeFromContext(userContext); len(owned) > 0 {
		out = DropOwnedProductSuggestions(out, owned)
	}
	return out
}

func parseWardrobeFromContext(userContext string) []wardrobeItem {
	if !strings.Contains(userContext, "## Wardrobe") {
		return nil
	}
	var items []wardrobeItem
	for _, line := range strings.Split(userContext, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		if !strings.Contains(line, "brand:") {
			continue
		}
		// "- Name | brand: X | category: Y"
		parts := strings.Split(strings.TrimPrefix(line, "- "), "|")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		var brand, category string
		for _, p := range parts[1:] {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "brand:") {
				brand = strings.TrimSpace(strings.TrimPrefix(p, "brand:"))
			}
			if strings.HasPrefix(p, "category:") {
				category = strings.TrimSpace(strings.TrimPrefix(p, "category:"))
			}
		}
		if name != "" && brand != "" {
			items = append(items, wardrobeItem{Name: name, Brand: brand, Category: category})
		}
	}
	return items
}

func matchCatalogEntry(
	s dto.ProductSuggestion,
	byLink map[string]affiliateCatalogEntry,
	byNameBrand map[string]affiliateCatalogEntry,
) (affiliateCatalogEntry, bool) {
	if link := normalizeAffiliateLink(s.AffiliateLink); link != "" {
		if e, ok := byLink[link]; ok {
			return e, true
		}
	}
	if key := nameBrandKey(s.ProductName, s.Brand); key != "" {
		if e, ok := byNameBrand[key]; ok {
			return e, true
		}
	}
	return affiliateCatalogEntry{}, false
}

func normalizeAffiliateLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.ToLower(raw)
	}
	u.Fragment = ""
	u.RawQuery = ""
	return strings.ToLower(strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"))
}

func nameBrandKey(productName, brand string) string {
	p := strings.ToLower(strings.TrimSpace(productName))
	b := strings.ToLower(strings.TrimSpace(brand))
	if p == "" || b == "" {
		return ""
	}
	return p + "|" + b
}

func normalizePriority(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high":
		return "high"
	default:
		return "medium"
	}
}

func priorityRank(p string) int {
	if p == "high" {
		return 2
	}
	return 1
}

func catalogStep(e affiliateCatalogEntry) string {
	if s := normLower(e.Step); s != "" {
		return s
	}
	switch normLower(e.Category) {
	case "cleanser":
		return "cleanse"
	case "moisturizer":
		return "moisturize"
	case "spf":
		return "spf"
	case "toner", "serum":
		if normLower(e.ActiveKind) == "bha" || normLower(e.ActiveKind) == "bp" || normLower(e.ActiveKind) == "aha" {
			return "treat"
		}
		return "soothe"
	case "treatment":
		return "treat"
	}
	return ""
}

func defaultBenefitsForStep(step, locale string) []string {
	en := strings.EqualFold(locale, "en")
	switch normLower(step) {
	case "cleanse":
		if en {
			return []string{"Gentle cleanse", "Less tightness"}
		}
		return []string{"Làm sạch nhẹ", "Ít làm da căng"}
	case "moisturize":
		if en {
			return []string{"Keeps moisture", "Supports steady skin"}
		}
		return []string{"Giữ ẩm", "Hỗ trợ da ổn định"}
	case "spf":
		if en {
			return []string{"Daily protection", "Helps limit new dark marks"}
		}
		return []string{"Bảo vệ mỗi sáng", "Giảm nguy cơ thâm mới"}
	case "soothe":
		if en {
			return []string{"Soothes", "Preps for moisturizer"}
		}
		return []string{"Làm dịu", "Chuẩn bị cho kem dưỡng"}
	case "treat":
		if en {
			return []string{"Targets the concern", "One change at a time"}
		}
		return []string{"Nhắm đúng vấn đề", "Chỉ 1 thay đổi mỗi lần"}
	}
	return nil
}

func defaultHowToUseForStep(step, locale string) string {
	en := strings.EqualFold(locale, "en")
	switch normLower(step) {
	case "cleanse":
		if en {
			return "Lukewarm water, ~30 seconds, pat dry."
		}
		return "Nước ấm, khoảng 30 giây, thấm khô."
	case "moisturize":
		if en {
			return "Thin layer after cleansing (and after treat if used)."
		}
		return "Lớp mỏng sau rửa mặt (và sau treat nếu có)."
	case "spf":
		if en {
			return "Every morning, including near windows indoors."
		}
		return "Mỗi sáng, kể cả khi ở nhà gần cửa sổ."
	case "soothe":
		if en {
			return "Pat gently after cleansing; skip if it stings."
		}
		return "Vỗ nhẹ sau rửa; bỏ qua nếu đang châm."
	case "treat":
		if en {
			return "Start 2–3 nights/week on a small area; moisturize after."
		}
		return "Bắt đầu 2–3 đêm/tuần vùng nhỏ; dưỡng ẩm sau."
	}
	return ""
}

func defaultActiveCaution(locale string) string {
	if strings.EqualFold(locale, "en") {
		return "One active only; stop if redness/swelling increases. Not a prescription."
	}
	return "Chỉ 1 hoạt chất; ngưng nếu càng đỏ/sưng. Không phải kê đơn."
}

// FilterProductSuggestionsForPhase drops treat/actives when phase is calm_first.
func FilterProductSuggestionsForPhase(items []dto.ProductSuggestion, phase string) []dto.ProductSuggestion {
	if len(items) == 0 {
		return items
	}
	if phase != PhaseCalmFirst {
		// can_add_active: keep at most one treat
		var out []dto.ProductSuggestion
		treatSeen := false
		for _, s := range items {
			if normLower(s.Step) == "treat" {
				if treatSeen {
					continue
				}
				treatSeen = true
			}
			out = append(out, s)
		}
		return out
	}
	out := make([]dto.ProductSuggestion, 0, len(items))
	for _, s := range items {
		if normLower(s.Step) == "treat" {
			continue
		}
		id := strings.TrimSpace(s.ProductID)
		if id != "" {
			if _, ban := calmFirstBannedCatalogIDs[id]; ban {
				continue
			}
		}
		low := strings.ToLower(s.ProductName + " " + s.Brand)
		if strings.Contains(low, "bha") || strings.Contains(low, "aha") || strings.Contains(low, "benzoyl") {
			continue
		}
		out = append(out, s)
	}
	return out
}

// LookupCatalogByLink resolves a catalog id from an affiliate URL (admin metrics).
func LookupCatalogByLink(link string) (id, name, brand string, ok bool) {
	rows, err := loadAffiliateCatalog()
	if err != nil {
		return "", "", "", false
	}
	want := normalizeAffiliateLink(link)
	for _, r := range rows {
		if normalizeAffiliateLink(r.AffiliateLink) == want && want != "" {
			return r.ID, r.ProductName, r.Brand, true
		}
	}
	return "", "", "", false
}
