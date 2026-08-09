package ai

import (
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

// BuildManualProductGuidance builds product guidance for the no-photo onboarding
// path, where the only signal is what the user picked: goal, skin type, concerns.
//
// Phase is always calm_first: without a photo we cannot judge how reactive the skin
// is, so no active is ever pushed. Copy is separate from guidanceTemplates because
// the calm_first wording assumes a visible flare ("da đang đỏ / sưng dày"), which
// would be wrong for someone who never uploaded a photo.
func BuildManualProductGuidance(
	goal string,
	skinType string,
	concerns []string,
	locale string,
) ([]dto.ProductGuidanceItem, []dto.ProductSuggestion) {
	merged := mergeGoalConcerns(goal, concerns)
	guidance, suggestions := attachCatalogToTemplates(
		manualGuidanceTemplates(goal, skinType, locale),
		PhaseCalmFirst, skinType, merged, nil, locale,
	)
	guidance = enrichGuidanceCopy(
		guidance, PhaseCalmFirst, "", merged, nil, nil, locale,
	)
	return guidance, suggestions
}

// mergeGoalConcerns folds goal-derived catalog vocabulary into the picked concern
// ids so a user who only chose a goal still matches sensible SKUs.
func mergeGoalConcerns(goal string, concerns []string) []string {
	seen := make(map[string]struct{}, len(concerns)+4)
	out := make([]string, 0, len(concerns)+4)
	add := func(v string) {
		v = normLower(v)
		if v == "" {
			return
		}
		if _, dup := seen[v]; dup {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, c := range concerns {
		add(c)
	}
	for _, n := range onboardingConcernNeedles(goal, nil) {
		add(n)
	}
	return out
}

func manualGuidanceTemplates(goal, skinType, locale string) []guidanceTemplate {
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		return manualTemplatesEN(goal, skinType)
	}
	return manualTemplatesVI(goal, skinType)
}

func manualTemplatesVI(goal, skinType string) []guidanceTemplate {
	return []guidanceTemplate{
		{
			Step: "cleanse", Category: "cleanser",
			NameOrCategory: manualCleanserLabelVI(skinType),
			Why:            manualCleanseWhyVI(skinType),
			Benefits:       []string{"Gỡ bụi, dầu và kem chống nắng", "Ít khô căng sau khi rửa"},
			HowToUse:       "Nước ấm, khoảng 30 giây, thấm khô — sáng và tối.",
			Caution:        "Đừng chà mạnh và đừng rửa quá 2 lần/ngày.",
		},
		{
			Step: "soothe", Category: "toner",
			NameOrCategory: "Lớp cấp ẩm / làm dịu (tuỳ chọn)",
			Why:            manualSootheWhyVI(goal),
			Benefits:       []string{"Giúp da đỡ khô căng", "Chuẩn bị cho kem dưỡng"},
			HowToUse:       "Vỗ nhẹ khi da còn hơi ẩm; bỏ qua nếu thấy châm.",
			Caution:        "Tuần đầu chưa thêm hoạt chất mạnh — chưa biết da bạn phản ứng thế nào.",
		},
		{
			Step: "moisturize", Category: "moisturizer",
			NameOrCategory: manualMoisturizerLabelVI(skinType),
			Why:            manualMoisturizeWhyVI(goal),
			Benefits:       []string{"Giữ da êm cả ngày", "Nền ổn trước khi tính tới hoạt chất"},
			HowToUse:       "Một lớp mỏng sau rửa mặt, sáng và tối.",
		},
		{
			Step: "spf", Category: "spf",
			NameOrCategory: "Kem chống nắng buổi sáng",
			Why:            manualSPFWhyVI(goal),
			Benefits:       []string{"Giảm thâm mới hình thành", "Bảo vệ kể cả khi ở trong nhà"},
			HowToUse:       "Mỗi sáng, kể cả ngày râm hoặc khi ngồi gần cửa sổ.",
		},
	}
}

func manualCleanserLabelVI(skinType string) string {
	switch normLower(skinType) {
	case "oily", "combo", "combination":
		return "Sữa rửa mặt gel/foam dịu"
	case "dry":
		return "Sữa rửa mặt dạng kem"
	case "sensitive":
		return "Sữa rửa mặt dịu, không mùi"
	}
	return "Sữa rửa mặt dịu"
}

func manualCleanserLabelEN(skinType string) string {
	switch normLower(skinType) {
	case "oily", "combo", "combination":
		return "Gentle gel / foam cleanser"
	case "dry":
		return "Cream cleanser"
	case "sensitive":
		return "Fragrance-free gentle cleanser"
	}
	return "Gentle cleanser"
}

func manualMoisturizerLabelVI(skinType string) string {
	switch normLower(skinType) {
	case "oily", "combo", "combination":
		return "Kem dưỡng nhẹ, không dầu"
	case "dry":
		return "Kem dưỡng cấp ẩm dày hơn"
	case "sensitive":
		return "Kem dưỡng tối giản"
	}
	return "Kem dưỡng ẩm"
}

func manualMoisturizerLabelEN(skinType string) string {
	switch normLower(skinType) {
	case "oily", "combo", "combination":
		return "Light oil-free moisturizer"
	case "dry":
		return "Richer hydrating moisturizer"
	case "sensitive":
		return "Simple moisturizer"
	}
	return "Moisturizer"
}

func manualCleanseWhyVI(skinType string) string {
	switch normLower(skinType) {
	case "oily", "combo", "combination":
		return "Da bạn tiết dầu nhiều hơn, nên bước rửa cần sạch thật nhưng không làm da khô ngược rồi tiết dầu bù."
	case "dry":
		return "Da khô mất êm rất nhanh nếu rửa mạnh — dạng kem giữ lại lớp dầu tự nhiên cần thiết."
	case "sensitive":
		return "Da dễ kích ứng thì hương liệu là thủ phạm phổ biến nhất — công thức càng ngắn càng an toàn."
	}
	return "Bước nền của mọi routine: làm sạch đủ mà không khiến da căng sau khi rửa."
}

func manualCleanseWhyEN(skinType string) string {
	switch normLower(skinType) {
	case "oily", "combo", "combination":
		return "Your skin runs oilier, so cleansing should actually clean without drying it into producing more oil."
	case "dry":
		return "Dry skin loses comfort fast with a harsh wash — a cream formula keeps the natural oils you need."
	case "sensitive":
		return "Fragrance is the most common trigger for easily irritated skin — the shorter the formula, the safer."
	}
	return "The base of any routine: clean enough without leaving skin tight afterwards."
}

func manualSootheWhyVI(goal string) string {
	switch normLower(goal) {
	case "clear_acne":
		return "Bạn chọn mục tiêu giảm mụn — tuần đầu ưu tiên làm dịu, chưa đẩy BHA/BP, để biết da phản ứng ra sao trước đã."
	case "barrier":
		return "Bạn chọn mục tiêu phục hồi — một lớp cấp ẩm nhẹ giúp da bớt căng và đỡ đỏ mà không thêm gánh nặng."
	case "glow":
		return "Bạn chọn mục tiêu da sáng khoẻ — đủ ẩm là cách nhanh nhất để da trông tươi hơn, trước cả vitamin C."
	case "anti_aging":
		return "Bạn chọn mục tiêu chống lão hoá — cấp ẩm đều đặn là nền bắt buộc trước khi tính tới retinol."
	}
	return "Chọn theo mục tiêu và vấn đề bạn tick — cấp ẩm nhẹ là bước an toàn nhất cho tuần đầu."
}

func manualSootheWhyEN(goal string) string {
	switch normLower(goal) {
	case "clear_acne":
		return "You picked clearing breakouts — week one stays soothing, no BHA/BP push, so you learn how your skin reacts first."
	case "barrier":
		return "You picked recovery — a light hydrating layer eases tightness and redness without adding load."
	case "glow":
		return "You picked healthy glow — being properly hydrated makes skin look fresher faster than any vitamin C."
	case "anti_aging":
		return "You picked gentle anti-aging — steady hydration is the required base before you even consider retinol."
	}
	return "Chosen from your goal and the concerns you ticked — light hydration is the safest first-week step."
}

func manualMoisturizeWhyVI(goal string) string {
	switch normLower(goal) {
	case "clear_acne":
		return "Da đang có mụn vẫn cần dưỡng: thiếu ẩm làm da tiết dầu bù và dễ bít hơn, không phải ít dưỡng là bớt mụn."
	case "barrier":
		return "Đây là bước quan trọng nhất với da dễ kích ứng — chọn công thức ngắn, không cồn khô, không hương liệu."
	case "glow":
		return "Da đủ ẩm phản xạ ánh sáng tốt hơn nên trông căng và sáng — độ sáng bắt đầu từ đây chứ không từ tẩy da chết."
	case "anti_aging":
		return "Nền ẩm tốt giúp da chịu được hoạt chất về sau mà không bong tróc, nên làm trước khi thêm retinol."
	}
	return "Giữ da êm là nền cho mọi mục tiêu tiếp theo — bỏ bước này thì các bước khác khó ăn thua."
}

func manualMoisturizeWhyEN(goal string) string {
	switch normLower(goal) {
	case "clear_acne":
		return "Breakout-prone skin still needs moisture: skipping it makes skin overproduce oil and clog more, not less."
	case "barrier":
		return "This is the single most important step for easily irritated skin — short formula, no drying alcohol, no fragrance."
	case "glow":
		return "Hydrated skin reflects light better, so it looks plump and bright — glow starts here, not with exfoliation."
	case "anti_aging":
		return "A solid moisture base is what lets skin tolerate actives later without flaking, so do it before adding retinol."
	}
	return "Keeping skin comfortable is the base for every goal after this — skip it and the rest struggles."
}

func manualSPFWhyVI(goal string) string {
	switch normLower(goal) {
	case "clear_acne":
		return "Thâm sau mụn đậm lên chủ yếu vì nắng — chống nắng là thứ giữ cho vết cũ mờ dần thay vì đậm thêm."
	case "anti_aging":
		return "Đây là bước chống lão hoá hiệu quả nhất và rẻ nhất, hơn mọi serum bạn có thể mua."
	case "glow":
		return "Không có bước này thì mọi nỗ lực làm sáng đều bị nắng kéo ngược lại."
	}
	return "Bước ảnh hưởng rõ nhất tới thâm và tông da về lâu dài — làm đều còn hơn làm nhiều."
}

func manualSPFWhyEN(goal string) string {
	switch normLower(goal) {
	case "clear_acne":
		return "Post-acne marks darken mostly from sun — sunscreen is what lets old spots fade instead of deepening."
	case "anti_aging":
		return "This is the most effective and cheapest anti-aging step there is, ahead of any serum you can buy."
	case "glow":
		return "Without this step, every brightening effort gets dragged back by the sun."
	}
	return "The step with the clearest long-term effect on marks and tone — consistency beats quantity."
}

func manualTemplatesEN(goal, skinType string) []guidanceTemplate {
	return []guidanceTemplate{
		{
			Step: "cleanse", Category: "cleanser",
			NameOrCategory: manualCleanserLabelEN(skinType),
			Why:            manualCleanseWhyEN(skinType),
			Benefits:       []string{"Removes dirt, oil and sunscreen", "Less tightness after washing"},
			HowToUse:       "Lukewarm water, about 30 seconds, pat dry — morning and evening.",
			Caution:        "No hard scrubbing, and no more than twice a day.",
		},
		{
			Step: "soothe", Category: "toner",
			NameOrCategory: "Hydrating / soothing layer (optional)",
			Why:            manualSootheWhyEN(goal),
			Benefits:       []string{"Eases dry, tight feeling", "Preps skin for moisturizer"},
			HowToUse:       "Pat on while skin is still damp; skip it if it stings.",
			Caution:        "No strong actives in week one — you do not know yet how your skin reacts.",
		},
		{
			Step: "moisturize", Category: "moisturizer",
			NameOrCategory: manualMoisturizerLabelEN(skinType),
			Why:            manualMoisturizeWhyEN(goal),
			Benefits:       []string{"Keeps skin comfortable all day", "A steady base before any active"},
			HowToUse:       "A thin layer after cleansing, morning and evening.",
		},
		{
			Step: "spf", Category: "spf",
			NameOrCategory: "Morning sunscreen",
			Why:            manualSPFWhyEN(goal),
			Benefits:       []string{"Limits new dark marks", "Protects even when you stay indoors"},
			HowToUse:       "Every morning, including cloudy days or when sitting near a window.",
		},
	}
}
