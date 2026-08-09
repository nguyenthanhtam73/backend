package ai

import (
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

// Catalog IDs that push BHA/AHA/BP-style actives — banned for calm_first phase.
var calmFirstBannedCatalogIDs = map[string]struct{}{
	"some-by-mi-miracle-toner": {},
	"to-niacinamide":           {}, // treat-like; keep for can_add_active only
	"melano-cc-premium":        {},
	"axis-y-dark-spot":         {},
}

// BuildOnboardingProductGuidance builds hybrid product guidance for analyze-skin.
// Always returns generic role cards; affiliate fields only when a catalog row matches
// category + phase + concerns (links rewritten from catalog).
func BuildOnboardingProductGuidance(
	phase string,
	severity string,
	skinType string,
	concerns []string,
	concernTypes []string,
	locale string,
) ([]dto.ProductGuidanceItem, []dto.ProductSuggestion) {
	if phase != PhaseCalmFirst && phase != PhaseCanAddActive {
		phase = PhaseCalmFirst
	}
	if severity == SeverityDense {
		phase = PhaseCalmFirst
	}
	templates := guidanceTemplates(phase, locale)
	rows, _ := loadAffiliateCatalog()

	out := make([]dto.ProductGuidanceItem, 0, len(templates))
	var suggestions []dto.ProductSuggestion
	usedIDs := map[string]struct{}{}
	affiliateSlots := 0 // commerce CTAs capped at maxProductSuggestions (0–2)

	for _, tmpl := range templates {
		item := tmpl
		item.Phase = phase
		// Always keep generic role label — brand names only on commerce fields when linked.
		if entry, ok := matchGuidanceCatalog(rows, item.Step, item.Category, phase, skinType, concerns, concernTypes, usedIDs); ok {
			usedIDs[entry.ID] = struct{}{}
			if affiliateSlots < maxProductSuggestions {
				affiliateSlots++
				item.AffiliateProductID = entry.ID
				item.ProductName = entry.ProductName
				item.Brand = entry.Brand
				item.AffiliateLink = entry.AffiliateLink
				item.PriceRange = entry.PriceRange
				reason := strings.TrimSpace(item.Why)
				if reason == "" {
					reason = applyReasonTemplate(entry.ReasonTemplate, concerns, concernTypes, locale)
				}
				if reason == "" {
					reason = entry.ProductName
				}
				suggestions = append(suggestions, dto.ProductSuggestion{
					ProductName:   entry.ProductName,
					Brand:         entry.Brand,
					Reason:        reason,
					AffiliateLink: entry.AffiliateLink,
					PriceRange:    entry.PriceRange,
					Priority:      "high",
					ProductID:     entry.ID,
					Step:          catalogStep(entry),
				})
			}
			// Else: catalog matched but CTA budget exhausted — keep text-only role card.
		}
		out = append(out, item)
	}
	return out, SanitizeProductSuggestionsLocale(suggestions, locale)
}

func applyReasonTemplate(tmpl string, concerns, concernTypes []string, locale string) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}
	concern := ""
	if len(concernTypes) > 0 {
		concern = concernTypes[0]
	} else if len(concerns) > 0 {
		concern = concerns[0]
	}
	if concern == "" {
		if strings.EqualFold(locale, "en") {
			concern = "your skin concerns"
		} else {
			concern = "vấn đề da bạn chọn"
		}
	}
	return strings.ReplaceAll(tmpl, "{concern}", concern)
}

type guidanceTemplate = dto.ProductGuidanceItem

func guidanceTemplates(phase, locale string) []guidanceTemplate {
	en := strings.EqualFold(locale, "en")
	if phase == PhaseCanAddActive {
		if en {
			return []guidanceTemplate{
				{
					Step: "cleanse", Category: "cleanser", NameOrCategory: "Gentle gel / foam cleanser",
					Why: "Clears oil and residue without stripping — keeps the base calm enough for one active later.",
					Benefits: []string{"Removes sunscreen and oil", "Less tightness after washing"},
					HowToUse: "Lukewarm water, ~30 seconds, pat dry — morning and evening.",
					Caution:  "Skip harsh scrubs while you introduce an active.",
				},
				{
					Step: "moisturize", Category: "moisturizer", NameOrCategory: "Light moisturizer",
					Why: "Keeps comfort around any active so skin does not feel tight or angry.",
					Benefits: []string{"Supports the skin barrier", "Makes actives easier to tolerate"},
					HowToUse: "Thin layer after cleanse (and after active if you use one).",
				},
				{
					Step: "spf", Category: "spf", NameOrCategory: "Morning sunscreen",
					Why: "Daily protection matters even more when treating breakouts or marks.",
					Benefits: []string{"Protects healing skin", "Helps prevent new dark marks"},
					HowToUse: "Every morning, including near windows indoors.",
				},
				{
					Step: "treat", Category: "treatment", NameOrCategory: "One active (BHA or BP — not both)",
					Why: "Skin looks steady enough for a single active focused on clogged pores / spots — do not stack.",
					Benefits: []string{"Targets breakouts gradually", "One product change at a time"},
					HowToUse: "Start 2–3 nights/week on a small area; moisturize after.",
					Caution:  "Not a 7-day clear promise. Stop if stinging or swelling increases. Not a prescription.",
				},
			}
		}
		return []guidanceTemplate{
			{
				Step: "cleanse", Category: "cleanser", NameOrCategory: "Sữa rửa mặt dịu (gel/foam nhẹ)",
				Why: "Làm sạch dầu và lớp kem chống nắng mà không làm da căng — nền ổn để sau đó dùng 1 hoạt chất.",
				Benefits: []string{"Gỡ bẩn / dầu nhẹ", "Ít bị khô căng sau rửa"},
				HowToUse: "Nước ấm, khoảng 30 giây, thấm khô — sáng và tối.",
				Caution:  "Tránh chà mạnh khi đang thêm hoạt chất.",
			},
			{
				Step: "moisturize", Category: "moisturizer", NameOrCategory: "Kem dưỡng ẩm nhẹ",
				Why: "Giữ da êm quanh hoạt chất, giảm cảm giác căng / dễ đỏ.",
				Benefits: []string{"Hỗ trợ da ổn định", "Dễ chịu hơn khi có treat"},
				HowToUse: "Lớp mỏng sau rửa mặt (và sau treat nếu có).",
			},
			{
				Step: "spf", Category: "spf", NameOrCategory: "Kem chống nắng buổi sáng",
				Why: "Bảo vệ da đang phục hồi và giảm nguy cơ thâm mới.",
				Benefits: []string{"Bảo vệ da mỗi ngày", "Giảm thâm sau mụn"},
				HowToUse: "Mỗi sáng, kể cả khi ở nhà gần cửa sổ.",
			},
			{
				Step: "treat", Category: "treatment", NameOrCategory: "1 hoạt chất (BHA hoặc BP — không stack)",
				Why: "Da đang ổn định hơn — có thể thêm đúng 1 active cho mụn / tắc nghẽn, không dùng chung BHA+BP.",
				Benefits: []string{"Nhắm vào nốt / tắc nghẽn dần", "Đổi một thứ một lúc"},
				HowToUse: "Bắt đầu 2–3 đêm/tuần, vùng nhỏ; dưỡng ẩm sau.",
				Caution:  "Không hứa hết mụn 7 ngày. Ngưng nếu càng đỏ/sưng. Không phải kê đơn.",
			},
		}
	}

	// calm_first
	if en {
		return []guidanceTemplate{
			{
				Step: "cleanse", Category: "cleanser", NameOrCategory: "Fragrance-free gentle cleanser",
				Why: "When skin looks hot / inflamed, cleansing should soothe — not scrub or strip.",
				Benefits: []string{"Removes dirt without friction", "Less sting after washing"},
				HowToUse: "Lukewarm water only; soft press, no scrubbing inflamed spots.",
				Caution:  "Do not pick or squeeze. No BHA/BP push in this phase.",
			},
			{
				Step: "soothe", Category: "toner", NameOrCategory: "Soothing hydrating layer",
				Why: "A calm layer helps redness and tightness settle before any actives.",
				Benefits: []string{"Comfort on irritated areas", "Preps skin for moisturizer"},
				HowToUse: "Pat gently; skip if it stings.",
			},
			{
				Step: "moisturize", Category: "moisturizer", NameOrCategory: "Barrier-support moisturizer",
				Why: "Dense or angry-looking spots need comfort and repair signals first.",
				Benefits: []string{"Reduces tight, dry feel", "Supports recovery"},
				HowToUse: "Generous on red / dry zones; keep formula simple.",
				Caution:  "Avoid stacking new strong actives this week.",
			},
			{
				Step: "spf", Category: "spf", NameOrCategory: "Gentle morning sunscreen",
				Why: "Protection while skin is reactive — especially on cheeks and healing areas.",
				Benefits: []string{"Shields inflamed skin", "Helps limit new dark marks"},
				HowToUse: "Every morning; mineral options if chemical filters sting.",
			},
		}
	}
	return []guidanceTemplate{
		{
			Step: "cleanse", Category: "cleanser", NameOrCategory: "Sữa rửa mặt dịu, không mùi",
			Why: "Khi da đang đỏ / sưng dày, bước rửa cần nhẹ — không chà, không đẩy acid.",
			Benefits: []string{"Làm sạch nhẹ", "Ít châm chích sau rửa"},
			HowToUse: "Nước ấm; miết nhẹ, không nặn / không cậy nốt.",
			Caution:  "Pha này không đẩy BHA/BP. Không nặn mụn.",
		},
		{
			Step: "soothe", Category: "toner", NameOrCategory: "Lớp làm dịu / cấp ẩm nhẹ",
			Why: "Giúp vùng đỏ và căng dịu lại trước khi nghĩ tới hoạt chất.",
			Benefits: []string{"Êm vùng đang kích", "Chuẩn bị cho kem dưỡng"},
			HowToUse: "Vỗ nhẹ; bỏ qua nếu đang châm.",
		},
		{
			Step: "moisturize", Category: "moisturizer", NameOrCategory: "Kem dưỡng hỗ trợ barrier",
			Why: "Mụn viêm dày / da “nóng” cần lớp phục hồi trước, chưa phải treat mạnh.",
			Benefits: []string{"Giảm khô căng", "Hỗ trợ da ổn lại"},
			HowToUse: "Thoa đủ trên vùng đỏ / khô; công thức ngắn.",
			Caution:  "Tuần này chưa stack hoạt chất mạnh.",
		},
		{
			Step: "spf", Category: "spf", NameOrCategory: "Kem chống nắng dịu buổi sáng",
			Why: "Bảo vệ khi da đang nhạy — đặc biệt má và vùng vừa viêm.",
			Benefits: []string{"Che nắng cho da đang kích", "Giảm nguy cơ thâm mới"},
			HowToUse: "Mỗi sáng; ưu tiên loại dịu nếu da dễ châm.",
		},
	}
}

func matchGuidanceCatalog(
	rows []affiliateCatalogEntry,
	step, category, phase, skinType string,
	concerns, concernTypes []string,
	used map[string]struct{},
) (affiliateCatalogEntry, bool) {
	wantCats := guidanceWantCategories(step, category)
	if len(wantCats) == 0 {
		return affiliateCatalogEntry{}, false
	}
	skin := mapSkinForCatalog(skinType)
	concernSet := guidanceConcernSet(concerns, concernTypes)

	var best affiliateCatalogEntry
	bestScore := 0
	for _, r := range rows {
		if _, dup := used[r.ID]; dup {
			continue
		}
		rStep := catalogStep(r)
		if rStep != "" && rStep != normLower(step) {
			continue
		}
		if rStep == "" && !categoryInList(r.Category, wantCats) {
			continue
		}
		if phase == PhaseCalmFirst {
			if normLower(step) == "treat" || rStep == "treat" {
				continue
			}
			if _, ban := calmFirstBannedCatalogIDs[r.ID]; ban {
				continue
			}
			if isActiveHeavyCatalogEntry(r) {
				continue
			}
		}
		if step == "treat" && phase != PhaseCanAddActive {
			continue
		}
		if len(r.Phases) > 0 && !phaseAllowedOnEntry(r.Phases, phase) {
			continue
		}
		score := 1
		if rStep == normLower(step) {
			score += 2
		}
		if skinMatchesCatalog(r.SkinTypes, skin) {
			score += 2
		}
		score += concernOverlapScore(r.Concerns, concernSet)
		// Prefer non-actives slightly in calm_first already enforced; in can_add_active prefer matching active_kind for treat
		if step == "treat" && (normLower(r.ActiveKind) == "bha" || normLower(r.ActiveKind) == "bp") {
			score += 1
		}
		if score > bestScore {
			bestScore = score
			best = r
		}
	}
	if bestScore == 0 || best.ID == "" {
		return affiliateCatalogEntry{}, false
	}
	// Require at least weak concern or skin match for treat; base steps can match category-only.
	if step == "treat" && bestScore < 2 {
		return affiliateCatalogEntry{}, false
	}
	return best, true
}

func guidanceWantCategories(step, category string) []string {
	c := normLower(category)
	switch normLower(step) {
	case "cleanse":
		return []string{"cleanser"}
	case "moisturize":
		return []string{"moisturizer"}
	case "spf":
		return []string{"spf"}
	case "soothe":
		if c == "serum" {
			return []string{"serum", "toner"}
		}
		return []string{"toner", "serum"}
	case "treat":
		return []string{"treatment", "toner", "serum"}
	}
	if c != "" {
		return []string{c}
	}
	return nil
}

func categoryInList(cat string, want []string) bool {
	c := normLower(cat)
	for _, w := range want {
		if c == w {
			return true
		}
	}
	return false
}

func mapSkinForCatalog(skinType string) string {
	switch normLower(skinType) {
	case "combo":
		return "combination"
	case "prefer_not", "":
		// Unknown / declined — no skin bonus (do not map to "normal").
		return ""
	default:
		return normLower(skinType)
	}
}

func skinMatchesCatalog(types []string, skin string) bool {
	if skin == "" || len(types) == 0 {
		return false
	}
	for _, t := range types {
		tt := normLower(t)
		if tt == skin {
			return true
		}
		// Mapped aliases only — do NOT treat "normal" as a universal wildcard.
		if skin == "combination" && (tt == "combo" || tt == "combination") {
			return true
		}
		if skin == "combo" && (tt == "combo" || tt == "combination") {
			return true
		}
	}
	return false
}

func guidanceConcernSet(concerns, concernTypes []string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, c := range concerns {
		set[normLower(c)] = struct{}{}
		switch normLower(c) {
		case "acne":
			set["acne"] = struct{}{}
			set["breakout"] = struct{}{}
			set["clogged_pores"] = struct{}{}
		case "redness", "weak_barrier":
			set["redness"] = struct{}{}
			set["sensitive"] = struct{}{}
			set["weak_barrier"] = struct{}{}
			set["irritated"] = struct{}{}
		case "dryness", "dehydration":
			set["dry"] = struct{}{}
			set["dehydrated"] = struct{}{}
		case "hyperpigmentation":
			set["hyperpigmentation"] = struct{}{}
			set["pih"] = struct{}{}
			set["dull"] = struct{}{}
		case "large_pores":
			set["large_pores"] = struct{}{}
			set["oily"] = struct{}{}
		}
	}
	for _, ct := range concernTypes {
		switch normLower(ct) {
		case "inflammatory_acne", "comedones":
			set["acne"] = struct{}{}
			set["breakout"] = struct{}{}
		case "redness_irritation":
			set["redness"] = struct{}{}
			set["sensitive"] = struct{}{}
			set["irritated"] = struct{}{}
		case "pih", "uneven_tone":
			set["hyperpigmentation"] = struct{}{}
			set["pih"] = struct{}{}
		case "dryness":
			set["dry"] = struct{}{}
		case "oiliness", "large_pores":
			set["oily"] = struct{}{}
			set["large_pores"] = struct{}{}
		}
	}
	return set
}

func concernOverlapScore(catalogConcerns []string, want map[string]struct{}) int {
	n := 0
	for _, c := range catalogConcerns {
		if _, ok := want[normLower(c)]; ok {
			n++
		}
	}
	return n
}

func phaseAllowedOnEntry(phases []string, phase string) bool {
	for _, p := range phases {
		if normLower(p) == normLower(phase) {
			return true
		}
	}
	return false
}

func isActiveHeavyCatalogEntry(r affiliateCatalogEntry) bool {
	if normLower(r.ActiveKind) == "bha" || normLower(r.ActiveKind) == "bp" || normLower(r.ActiveKind) == "aha" {
		return true
	}
	name := strings.ToLower(r.ProductName + " " + r.Brand)
	for _, needle := range []string{"bha", "aha", "benzoyl", "bp ", "miracle toner", "niacinamide 10"} {
		if strings.Contains(name, needle) {
			return true
		}
	}
	return false
}

// StripAffiliateFromProductGuidance clears commerce fields but keeps generic guidance copy.
// NameOrCategory is forced back to a step/role label so Premium no_ads never shows brand names.
// Why/benefits/how-to are scrubbed if they mention the stripped brand or product name.
func StripAffiliateFromProductGuidance(items []dto.ProductGuidanceItem, locale string) []dto.ProductGuidanceItem {
	if len(items) == 0 {
		return items
	}
	out := make([]dto.ProductGuidanceItem, len(items))
	for i, it := range items {
		brand, product := it.Brand, it.ProductName
		it.Why = scrubBrandMentions(it.Why, brand, product, locale, scrubKindWhy)
		it.HowToUse = scrubBrandMentions(it.HowToUse, brand, product, locale, scrubKindHow)
		it.Caution = scrubBrandMentions(it.Caution, brand, product, locale, scrubKindCaution)
		if len(it.Benefits) > 0 {
			clean := make([]string, 0, len(it.Benefits))
			for _, b := range it.Benefits {
				if s := scrubBrandMentions(b, brand, product, locale, scrubKindBenefit); s != "" {
					clean = append(clean, s)
				}
			}
			it.Benefits = clean
		}
		it.AffiliateProductID = ""
		it.ProductName = ""
		it.Brand = ""
		it.AffiliateLink = ""
		it.PriceRange = ""
		it.NameOrCategory = genericRoleLabel(it.Step, it.Category, locale)
		out[i] = it
	}
	return out
}

type scrubKind int

const (
	scrubKindWhy scrubKind = iota
	scrubKindHow
	scrubKindCaution
	scrubKindBenefit
)

func scrubBrandMentions(text, brand, product, locale string, kind scrubKind) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	low := strings.ToLower(text)
	for _, needle := range []string{brand, product} {
		n := strings.TrimSpace(strings.ToLower(needle))
		if n == "" || len(n) < 3 {
			continue
		}
		if strings.Contains(low, n) {
			return scrubFallback(kind, locale)
		}
	}
	return text
}

func scrubFallback(kind scrubKind, locale string) string {
	en := strings.EqualFold(locale, "en")
	switch kind {
	case scrubKindHow:
		if en {
			return "Use gently as directed for this step."
		}
		return "Dùng nhẹ theo hướng dẫn cho bước này."
	case scrubKindCaution:
		if en {
			return "Stop if irritation increases. Not a prescription."
		}
		return "Ngưng nếu càng kích ứng. Không phải kê đơn."
	case scrubKindBenefit:
		if en {
			return "Supports this care step."
		}
		return "Hỗ trợ bước chăm sóc này."
	default:
		if en {
			return "Fits this step for your current skin phase."
		}
		return "Phù hợp bước này theo giai đoạn da hiện tại."
	}
}

func genericRoleLabel(step, category, locale string) string {
	en := strings.EqualFold(locale, "en")
	switch normLower(step) {
	case "cleanse":
		if en {
			return "Gentle cleanser"
		}
		return "Sữa rửa mặt dịu"
	case "moisturize":
		if en {
			return "Moisturizer"
		}
		return "Kem dưỡng ẩm"
	case "spf":
		if en {
			return "Morning sunscreen"
		}
		return "Kem chống nắng"
	case "soothe":
		if en {
			return "Soothing layer"
		}
		return "Lớp làm dịu"
	case "treat":
		if en {
			return "One active (optional)"
		}
		return "1 hoạt chất (tuỳ chọn)"
	}
	if c := strings.TrimSpace(category); c != "" {
		return c
	}
	if en {
		return "Product tip"
	}
	return "Gợi ý sản phẩm"
}
