package ai

import (
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

// GuidanceAffiliateForRoutineStep returns the first catalog product that
// BuildOnboardingProductGuidance currently selects for skinType and this step
// category. Routine requests have no care phase, so this uses product
// guidance's default phase. No matching guidance card leaves the step empty.
// Brands are never chosen here; the guidance function is the only picker.
func GuidanceAffiliateForRoutineStep(skinType, category, title string) (productID, productName, link string, ok bool) {
	items, _ := BuildOnboardingProductGuidance(PhaseCalmFirst, "", skinType, nil, nil, "")
	return firstGuidanceAffiliate(items, category, title)
}

// AttachRoutineStepAffiliatesForSkin keeps a unique catalog match, then fills
// any step that still has no product from product guidance for skinType.
func AttachRoutineStepAffiliatesForSkin(steps []dto.RoutineStep, skinType string) []dto.RoutineStep {
	out := AttachRoutineStepAffiliates(steps)
	if len(out) == 0 {
		return out
	}
	need := false
	for _, s := range out {
		if s.AffiliateProductID == nil {
			need = true
			break
		}
	}
	if !need {
		return out
	}
	items, _ := BuildOnboardingProductGuidance(PhaseCalmFirst, "", skinType, nil, nil, "")
	for i := range out {
		if out[i].AffiliateProductID != nil {
			continue
		}
		id, name, link, ok := firstGuidanceAffiliate(items, out[i].Category, out[i].Title)
		if !ok {
			continue
		}
		out[i].AffiliateProductID = stringPtr(id)
		out[i].ProductName = stringPtr(name)
		out[i].AffiliateLink = stringPtr(link)
	}
	return out
}

func firstGuidanceAffiliate(items []dto.ProductGuidanceItem, category, title string) (string, string, string, bool) {
	step := guidanceStepForRoutineCategory(routineCategoryForGuidance(category, title))
	if step == "" {
		return "", "", "", false
	}
	for _, it := range items {
		if normLower(it.Step) != step {
			continue
		}
		if id, name, link, ok := affiliateFromCatalogID(it.AffiliateProductID); ok {
			return id, name, link, true
		}
	}
	return "", "", "", false
}

func affiliateFromCatalogID(id string) (string, string, string, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", "", "", false
	}
	rows, err := loadAffiliateCatalog()
	if err != nil {
		return "", "", "", false
	}
	for _, r := range rows {
		if r.ID != id {
			continue
		}
		link, ok := catalogHTTPSLink(r.AffiliateLink)
		if !ok || strings.TrimSpace(r.ProductName) == "" {
			return "", "", "", false
		}
		return r.ID, strings.TrimSpace(r.ProductName), link, true
	}
	return "", "", "", false
}

// routineCategoryForGuidance uses an explicit category. Empty and "other"
// fall back to the same title guess routine suggest uses.
func routineCategoryForGuidance(category, title string) string {
	switch normLower(category) {
	case "", "other":
		return inferCategory(title)
	default:
		return normLower(category)
	}
}

// guidanceStepForRoutineCategory maps a routine category onto a product-guidance
// step. Categories guidance does not emit (mask, eye, serum, …) return "".
func guidanceStepForRoutineCategory(category string) string {
	switch normLower(category) {
	case "cleanser", "cleanse":
		return "cleanse"
	case "moisturizer", "moisturize", "moisturiser":
		return "moisturize"
	case "spf", "sunscreen":
		return "spf"
	case "treatment", "treat", "active":
		return "treat"
	case "toner":
		return "soothe"
	default:
		return ""
	}
}
