package ai

import (
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

// CareRoleFromText maps a care step / category / product label onto a wardrobe role.
// Aligned with the frontend cabinet-care matcher (cleanser / moisturizer / spf / …).
func CareRoleFromText(text string) string {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return ""
	}
	compact := strings.NewReplacer(" ", "", "_", "", "-", "").Replace(s)
	switch compact {
	case "cleanse", "cleanser":
		return "cleanser"
	case "moisturize", "moisturiser", "moisturizer", "soothe", "moisturizecalm":
		return "moisturizer"
	case "spf", "sunscreen":
		return "spf"
	case "treat", "treatment":
		return "treatment"
	case "toner":
		return "toner"
	case "serum":
		return "serum"
	case "mask":
		return "mask"
	}
	switch {
	case containsAny(s, "chống nắng", "chong nang", "sunscreen", "spf"):
		return "spf"
	case containsAny(s, "rửa mặt", "rua mat", "sữa rửa", "sua rua", "cleanser", "cleanse"):
		return "cleanser"
	case containsAny(s, "dưỡng ẩm", "duong am", "kem dưỡng", "kem duong", "moisturizer", "moistur"):
		return "moisturizer"
	case containsAny(s, "toner", "nước hoa hồng", "nuoc hoa hong"):
		return "toner"
	case containsAny(s, "serum", "tinh chất", "tinh chat"):
		return "serum"
	case containsAny(s, "mặt nạ", "mat na", "mask"):
		return "mask"
	case containsAny(s, "treatment", "điều trị", "dieu tri", "chấm mụn", "cham mun"):
		return "treatment"
	}
	return ""
}

func wardrobeCoveredRoles(owned []wardrobeItem) map[string]wardrobeItem {
	out := make(map[string]wardrobeItem)
	for _, item := range owned {
		role := CareRoleFromText(item.Category)
		if role == "" {
			role = CareRoleFromText(item.Name)
		}
		if role == "" || role == "other" {
			continue
		}
		if _, ok := out[role]; !ok {
			out[role] = item
		}
	}
	return out
}

func todayCareRoles(care []CoachCareSuggestion) []string {
	seen := map[string]bool{}
	var roles []string
	for _, c := range care {
		role := CareRoleFromText(c.Step)
		if role == "" || seen[role] {
			continue
		}
		seen[role] = true
		roles = append(roles, role)
	}
	return roles
}

func wardrobeCoversTodayRoles(owned []wardrobeItem, care []CoachCareSuggestion) bool {
	roles := todayCareRoles(care)
	if len(roles) == 0 {
		return false
	}
	covered := wardrobeCoveredRoles(owned)
	for _, role := range roles {
		if _, ok := covered[role]; !ok {
			return false
		}
	}
	return true
}

func suggestionCareRole(s dto.ProductSuggestion) string {
	if cat, ok := catalogCategoryFor(s); ok {
		if role := CareRoleFromText(cat); role != "" {
			return role
		}
	}
	if role := CareRoleFromText(s.Step); role != "" {
		return role
	}
	return CareRoleFromText(s.ProductName)
}

// DropCoveredRoleProductSuggestions removes affiliate picks whose role is already on the shelf.
func DropCoveredRoleProductSuggestions(suggestions []dto.ProductSuggestion, owned []wardrobeItem) []dto.ProductSuggestion {
	if len(suggestions) == 0 || len(owned) == 0 {
		return suggestions
	}
	covered := wardrobeCoveredRoles(owned)
	if len(covered) == 0 {
		return suggestions
	}
	out := make([]dto.ProductSuggestion, 0, len(suggestions))
	for _, s := range suggestions {
		if role := suggestionCareRole(s); role != "" {
			if _, ok := covered[role]; ok {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

func annotateCareWithOwnedCabinet(care []CoachCareSuggestion, owned []wardrobeItem) []CoachCareSuggestion {
	if len(care) == 0 || len(owned) == 0 {
		return care
	}
	covered := wardrobeCoveredRoles(owned)
	out := make([]CoachCareSuggestion, len(care))
	copy(out, care)
	for i := range out {
		role := CareRoleFromText(out[i].Step)
		item, ok := covered[role]
		if !ok || strings.TrimSpace(item.Name) == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		blob := strings.ToLower(out[i].Step + " " + out[i].Why)
		if strings.Contains(blob, strings.ToLower(name)) {
			continue
		}
		suffix := "Dùng " + name + " trong tủ."
		if strings.TrimSpace(out[i].Why) == "" {
			out[i].Why = suffix
			continue
		}
		out[i].Why = strings.TrimSpace(out[i].Why) + " " + suffix
	}
	return out
}

// ApplyCabinetFirstCare names owned cabinet products on care_suggestions and
// clears product_suggestions when the shelf already covers today's care roles.
func ApplyCabinetFirstCare(out *CoachStructuredOutput, userContext string) {
	if out == nil {
		return
	}
	NormalizeCareSuggestions(out)
	owned := parseWardrobeFromContext(userContext)
	if len(owned) == 0 {
		return
	}
	out.CareSuggestions = annotateCareWithOwnedCabinet(out.CareSuggestions, owned)
	if wardrobeCoversTodayRoles(owned, out.CareSuggestions) {
		out.ProductSuggestions = nil
		out.ProductGuidance = nil
		return
	}
	filtered := DropCoveredRoleProductSuggestions(out.ProductSuggestions, owned)
	if len(filtered) == len(out.ProductSuggestions) {
		return
	}
	out.ProductSuggestions = filtered
	if len(filtered) == 0 {
		out.ProductGuidance = nil
		return
	}
	out.ProductGuidance = ProductSuggestionsToGuidance(filtered, out.CarePhase, localeFromUserContext(userContext))
}
