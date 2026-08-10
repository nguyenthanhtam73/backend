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
// primaryRegions (e.g. cheeks) personalizes why/caution copy — optional.
func BuildOnboardingProductGuidance(
	phase string,
	severity string,
	skinType string,
	concerns []string,
	concernTypes []string,
	locale string,
	primaryRegions ...string,
) ([]dto.ProductGuidanceItem, []dto.ProductSuggestion) {
	if phase != PhaseCalmFirst && phase != PhaseCanAddActive {
		phase = PhaseCalmFirst
	}
	if severity == SeverityDense {
		phase = PhaseCalmFirst
	}
	regions := primaryRegions
	if len(regions) == 1 && strings.Contains(regions[0], ",") {
		// Defensive: allow a single comma-joined arg from older call sites.
		regions = splitCSV(regions[0])
	}
	guidance, suggestions := attachCatalogToTemplates(
		guidanceTemplates(phase, locale),
		phase, skinType, concerns, concernTypes, locale,
	)
	guidance = enrichGuidanceCopy(guidance, phase, severity, concerns, concernTypes, regions, locale)
	return guidance, suggestions
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// enrichGuidanceCopy ensures why/benefits/how/caution are filled with step-specific
// copy. Region / severity / concern context lives in the Step-2 summary — do not
// prefix every card with the same “Với vùng má, mức vừa…” opener.
func enrichGuidanceCopy(
	items []dto.ProductGuidanceItem,
	phase, severity string,
	concerns, concernTypes, regions []string,
	locale string,
) []dto.ProductGuidanceItem {
	if len(items) == 0 {
		return items
	}
	en := strings.EqualFold(locale, "en")
	// severity / concerns / regions intentionally unused here — context belongs
	// in the onboarding summary, not repeated on every guidance card.
	_, _, _, _ = severity, concerns, concernTypes, regions
	out := make([]dto.ProductGuidanceItem, len(items))
	for i, it := range items {
		it.Phase = phase
		if strings.TrimSpace(it.Why) == "" || isGenericWhy(it.Why, en) {
			it.Why = defaultWhyForStep(it.Step, phase, en)
		} else {
			it.Why = stripRepeatedContextPrefix(it.Why, en)
		}
		it.Benefits = ensureBenefits(it.Step, it.Benefits, phase, en)
		if strings.TrimSpace(it.HowToUse) == "" {
			it.HowToUse = defaultHowForStep(it.Step, en)
		}
		if strings.TrimSpace(it.Caution) == "" {
			it.Caution = defaultCautionForStep(it.Step, phase, en)
		}
		out[i] = it
	}
	return out
}

// stripRepeatedContextPrefix removes “Với …:” / “For …:” openers so cards keep
// only the step-specific clause (context already shown in the page summary).
func stripRepeatedContextPrefix(why string, en bool) string {
	why = strings.TrimSpace(why)
	if why == "" {
		return why
	}
	low := strings.ToLower(why)
	prefixes := []string{"với ", "for "}
	if en {
		prefixes = []string{"for ", "với "}
	}
	for _, p := range prefixes {
		if !strings.HasPrefix(low, p) {
			continue
		}
		rest := why[len(p):]
		// Cut at first em-dash / colon / "—" that separates context from advice.
		for _, sep := range []string{" — ", " – ", " - ", ": ", "—"} {
			if i := strings.Index(rest, sep); i > 0 && i < 80 {
				trimmed := strings.TrimSpace(rest[i+len(sep):])
				if trimmed != "" {
					// Capitalize first rune for EN after strip.
					if en && len(trimmed) > 0 {
						r := []rune(trimmed)
						r[0] = []rune(strings.ToUpper(string(r[0])))[0]
						return string(r)
					}
					return trimmed
				}
			}
		}
	}
	return why
}

func isGenericWhy(why string, en bool) bool {
	w := strings.ToLower(strings.TrimSpace(why))
	if w == "" {
		return true
	}
	generics := []string{
		"nên dùng sữa rửa mặt", "use a cleanser", "good cleanser",
		"nên dưỡng ẩm", "use moisturizer", "use sunscreen",
	}
	for _, g := range generics {
		if w == g || strings.HasPrefix(w, g) {
			return true
		}
	}
	_ = en
	return false
}

func defaultWhyForStep(step, phase string, en bool) string {
	switch normLower(step) {
	case "cleanse":
		if phase == PhaseCalmFirst {
			if en {
				return "Wash gently — don’t scrub or put strong acids on swollen spots."
			}
			return "Rửa nhẹ nhàng — đừng chà mạnh hay bôi acid lên chỗ đang sưng."
		}
		if en {
			return "Clear oil and sunscreen without leaving skin tight."
		}
		return "Làm sạch dầu và kem chống nắng mà không làm da căng."
	case "soothe":
		if en {
			return "Calm redness and tightness before any strong treatment products."
		}
		return "Làm dịu chỗ đỏ và căng trước khi dùng sản phẩm trị mạnh."
	case "moisturize":
		if phase == PhaseCalmFirst {
			if en {
				return "Ease redness and keep skin comfortable — strong treatment can wait."
			}
			return "Giúp da bớt đỏ và dễ chịu hơn — chưa cần sản phẩm trị mạnh."
		}
		if en {
			return "Moisturizer keeps skin comfortable if you add one treatment product."
		}
		return "Dưỡng ẩm giúp da êm nếu bạn đang dùng một sản phẩm trị."
	case "spf":
		if en {
			return "Morning sunscreen protects sensitive skin and helps prevent new dark marks."
		}
		return "Kem chống nắng mỗi sáng bảo vệ da đang nhạy và hạn chế thâm mới."
	case "treat":
		if en {
			return "Use at most one treatment product at night — never two strong ones together."
		}
		return "Tối đa một sản phẩm trị mỗi đêm — không dùng hai loại mạnh cùng lúc."
	}
	if en {
		return "Fits this care step for your skin right now."
	}
	return "Phù hợp bước này với tình trạng da hiện tại của bạn."
}

func ensureBenefits(step string, benefits []string, phase string, en bool) []string {
	clean := make([]string, 0, 4)
	for _, b := range benefits {
		b = strings.TrimSpace(b)
		if b != "" {
			clean = append(clean, b)
		}
	}
	if len(clean) >= 2 {
		if len(clean) > 4 {
			return clean[:4]
		}
		return clean
	}
	defs := guidanceDefaultBenefits(step, phase, en)
	for _, d := range defs {
		if len(clean) >= 4 {
			break
		}
		dup := false
		for _, c := range clean {
			if strings.EqualFold(c, d) {
				dup = true
				break
			}
		}
		if !dup {
			clean = append(clean, d)
		}
	}
	return clean
}

func guidanceDefaultBenefits(step, phase string, en bool) []string {
	switch normLower(step) {
	case "cleanse":
		if en {
			return []string{"Cleans gently", "Less likely to sting after washing", "Does not scrub swollen spots"}
		}
		return []string{"Làm sạch nhẹ", "Ít gây rát sau khi rửa", "Không chà lên chỗ đang sưng"}
	case "soothe":
		if en {
			return []string{"Calms redness", "Light hydration", "Preps for moisturizer"}
		}
		return []string{"Làm dịu cảm giác đỏ", "Cấp ẩm nhẹ", "Chuẩn bị cho kem dưỡng"}
	case "moisturize":
		if phase == PhaseCalmFirst {
			if en {
				return []string{"Eases redness", "Helps skin feel less tight", "Comfort on sore spots"}
			}
			return []string{"Làm dịu chỗ đang đỏ", "Giúp da đỡ khô căng", "Êm vùng đang sưng"}
		}
		if en {
			return []string{"Keeps skin comfortable", "Easier to tolerate one treatment", "Overnight comfort"}
		}
		return []string{"Giữ da dễ chịu", "Dễ chịu hơn khi có sản phẩm trị", "Êm da qua đêm"}
	case "spf":
		if en {
			return []string{"Daily sun protection", "Helps prevent new dark marks", "Shields sensitive skin"}
		}
		return []string{"Chống nắng mỗi ngày", "Hạn chế thâm mới sau mụn", "Bảo vệ da đang nhạy"}
	case "treat":
		if en {
			return []string{"Targets clogged pores gradually", "One change at a time", "Optional — skip if skin stings"}
		}
		return []string{"Giúp giảm tắc nghẽn dần", "Đổi một thứ một lúc", "Tuỳ chọn — bỏ nếu da rát"}
	}
	if en {
		return []string{"Supports this care step", "Fits your skin right now"}
	}
	return []string{"Hỗ trợ bước này", "Phù hợp tình trạng da hiện tại"}
}

func defaultHowForStep(step string, en bool) string {
	switch normLower(step) {
	case "cleanse":
		if en {
			return "Lukewarm water, about 30 seconds, soft press — morning and evening."
		}
		return "Nước ấm, khoảng 30 giây, miết nhẹ — sáng và tối."
	case "soothe":
		if en {
			return "Pat a thin layer; skip if it stings."
		}
		return "Vỗ lớp mỏng; bỏ qua nếu đang rát."
	case "moisturize":
		if en {
			return "Apply while skin is slightly damp; cover red or dry areas well."
		}
		return "Thoa khi da còn hơi ẩm; phủ đủ chỗ đỏ hoặc khô."
	case "spf":
		if en {
			return "Every morning as the last step — including near windows indoors."
		}
		return "Mỗi sáng, bước cuối — kể cả khi ở nhà gần cửa sổ."
	case "treat":
		if en {
			return "2–3 nights a week on a small area; moisturize after. Don’t use two strong treatments the same night."
		}
		return "2–3 đêm/tuần, vùng nhỏ; dưỡng ẩm sau. Không dùng hai sản phẩm trị mạnh cùng đêm."
	}
	if en {
		return "Use gently as directed for this step."
	}
	return "Dùng nhẹ theo hướng dẫn cho bước này."
}

func defaultCautionForStep(step, phase string, en bool) string {
	if phase == PhaseCalmFirst {
		if en {
			return "Focus on calming first: skip strong acne treatments this week. Don’t pick or squeeze. Not a medical prescription."
		}
		return "Ưu tiên làm dịu trước: tuần này chưa dùng sản phẩm trị mụn mạnh. Đừng nặn hay cậy mụn. Đây không phải đơn thuốc."
	}
	if normLower(step) == "treat" {
		if en {
			return "At most one treatment product per night. Stop if stinging or swelling increases. Not a medical prescription."
		}
		return "Tối đa một sản phẩm trị mỗi đêm. Ngưng nếu càng đỏ hoặc sưng. Đây không phải đơn thuốc."
	}
	if en {
		return "Add only one new product per week. Stop if irritation rises."
	}
	return "Mỗi tuần chỉ thêm 1 sản phẩm mới. Ngưng nếu da càng khó chịu."
}

// attachCatalogToTemplates fills commerce fields on role cards from the catalog,
// capped at maxProductSuggestions CTAs. Templates that win no match stay text-only.
func attachCatalogToTemplates(
	templates []guidanceTemplate,
	phase string,
	skinType string,
	concerns []string,
	concernTypes []string,
	locale string,
) ([]dto.ProductGuidanceItem, []dto.ProductSuggestion) {
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

	// calm_first — 3 cards (soothe folded into moisturize to keep mobile short).
	if en {
		return []guidanceTemplate{
			{
				Step: "cleanse", Category: "cleanser", NameOrCategory: "Gentle fragrance-free cleanser",
				Why: "Wash gently — don’t scrub or put strong acids on swollen spots.",
				Benefits: []string{"Cleans gently", "Less likely to sting after washing"},
				HowToUse: "Lukewarm water; soft press — don’t scrub swollen spots.",
				Caution:  "Don’t pick or squeeze. Skip strong acne treatments this week.",
			},
			{
				Step: "moisturize", Category: "moisturizer", NameOrCategory: "Soothing moisturizer",
				Why: "Ease redness and keep skin comfortable — strong treatment can wait.",
				Benefits: []string{"Eases redness", "Helps skin feel less tight", "Comfort on sore spots"},
				HowToUse: "Apply on slightly damp skin — cover red or dry areas well.",
				Caution:  "Calm first this week. Skip acids or retinol for now. Don’t pick.",
			},
			{
				Step: "spf", Category: "spf", NameOrCategory: "Gentle morning sunscreen",
				Why: "Morning sunscreen protects sensitive skin and helps prevent new dark marks.",
				Benefits: []string{"Daily sun protection", "Helps prevent new dark marks", "Shields sensitive skin"},
				HowToUse: "Every morning; choose a gentler formula if your skin stings easily.",
				Caution:  "Keep sunscreen even when skin is inflamed — pause strong treatments, not sun protection.",
			},
		}
	}
	return []guidanceTemplate{
		{
			Step: "cleanse", Category: "cleanser", NameOrCategory: "Sữa rửa mặt dịu, không mùi",
			Why: "Rửa nhẹ nhàng — đừng chà mạnh hay bôi acid lên chỗ đang sưng.",
			Benefits: []string{"Làm sạch nhẹ", "Ít gây rát sau khi rửa"},
			HowToUse: "Nước ấm; miết nhẹ — đừng nặn hay cậy nốt.",
			Caution:  "Tuần này chưa dùng sản phẩm trị mụn mạnh. Đừng nặn mụn.",
		},
		{
			Step: "moisturize", Category: "moisturizer", NameOrCategory: "Kem dưỡng làm dịu",
			Why: "Giúp da bớt đỏ và dễ chịu hơn — chưa cần sản phẩm trị mạnh.",
			Benefits: []string{"Làm dịu chỗ đang đỏ", "Giúp da đỡ khô căng", "Êm vùng đang sưng"},
			HowToUse: "Thoa khi da còn hơi ẩm — phủ đủ chỗ đỏ hoặc khô.",
			Caution:  "Ưu tiên làm dịu trước. Tuần này chưa dùng acid hay retinol. Đừng nặn.",
		},
		{
			Step: "spf", Category: "spf", NameOrCategory: "Kem chống nắng dịu buổi sáng",
			Why: "Kem chống nắng mỗi sáng bảo vệ da đang nhạy và hạn chế thâm mới.",
			Benefits: []string{"Chống nắng mỗi ngày", "Hạn chế thâm mới sau mụn", "Bảo vệ da đang nhạy"},
			HowToUse: "Mỗi sáng; chọn loại dịu nếu da hay bị rát.",
			Caution:  "Da đang viêm vẫn cần chống nắng. Tạm bỏ sản phẩm trị mạnh — đừng bỏ kem chống nắng.",
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
		// Treat CTAs must be BHA/BP actives — never a random toner/serum SKU.
		if normLower(step) == "treat" {
			ak := normLower(r.ActiveKind)
			if ak != "bha" && ak != "bp" {
				continue
			}
		}
		score := 1
		if rStep == normLower(step) {
			score += 2
		}
		if skinMatchesCatalog(r.SkinTypes, skin) {
			score += 2
		}
		score += concernOverlapScore(r.Concerns, concernSet)
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
		case "dullness":
			set["dull"] = struct{}{}
			set["dehydrated"] = struct{}{}
		case "uneven_texture":
			set["texture"] = struct{}{}
			set["dull"] = struct{}{}
			set["clogged_pores"] = struct{}{}
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
