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
	ID            string   `json:"id"`
	ProductName   string   `json:"product_name"`
	Brand         string   `json:"brand"`
	Category      string   `json:"category"`
	SkinTypes     []string `json:"skin_types"`
	Concerns      []string `json:"concerns"`
	PriceRange    string   `json:"price_range"`
	AffiliateLink string   `json:"affiliate_link"`
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
- If USER_MEMORY has ## Wardrobe listing products the user already owns → **do NOT** re-recommend those; suggest only a clear gap (e.g. missing SPF) or return [].
- "reason" MUST be specific to TODAY (tags, photo cues, profile goal, wardrobe gap) — warm friend tone, not ad copy.
- "priority": "high" = directly fills today's top gap; "medium" = optional add-on.
- Routine steps stay generic roles; product_suggestions is the ONLY branded slot.
- Skip affiliate picks when: stinging/redness flare, user 👎 affiliate picks often, wardrobe already complete, or no catalog item clearly fits.
- **Routine suggest:** if today's routine or tags show missing SPF / recent sun → product_suggestions SHOULD prioritize category "spf" from catalog (one pick max).
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
		ProductName   string   `json:"product_name"`
		Brand         string   `json:"brand"`
		Category      string   `json:"category"`
		SkinTypes     []string `json:"skin_types"`
		Concerns      []string `json:"concerns"`
		PriceRange    string   `json:"price_range"`
		AffiliateLink string   `json:"affiliate_link"`
	}
	out := make([]promptRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, promptRow{
			ProductName:   r.ProductName,
			Brand:         r.Brand,
			Category:      r.Category,
			SkinTypes:     r.SkinTypes,
			Concerns:      r.Concerns,
			PriceRange:    r.PriceRange,
			AffiliateLink: r.AffiliateLink,
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// SanitizeProductSuggestions validates AI output against the affiliate catalog.
// Unknown or hallucinated entries are dropped; links are always taken from catalog.
func SanitizeProductSuggestions(raw []dto.ProductSuggestion) []dto.ProductSuggestion {
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
			continue
		}
		out = append(out, dto.ProductSuggestion{
			ProductName:   entry.ProductName,
			Brand:         entry.Brand,
			Reason:        reason,
			AffiliateLink: entry.AffiliateLink,
			PriceRange:    entry.PriceRange,
			Priority:      normalizePriority(s.Priority),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return priorityRank(out[i].Priority) > priorityRank(out[j].Priority)
	})
	return out
}

// FinalizeProductSuggestions sanitizes catalog picks and drops items the user already owns
// when a ## Wardrobe block is present in userContext (USER_MEMORY).
func FinalizeProductSuggestions(raw []dto.ProductSuggestion, userContext string) []dto.ProductSuggestion {
	out := SanitizeProductSuggestions(raw)
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
