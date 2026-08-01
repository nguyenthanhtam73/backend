package profile

import (
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/service/ai"
)

// starterRoutineBGTimeout caps the background LLM job that personalizes the
// starter routine after CompleteOnboarding returns the quick scaffold.
const starterRoutineBGTimeout = 4 * time.Minute

func starterRoutineResponseFromAI(s ai.StarterRoutine) dto.StarterRoutineResponse {
	return dto.StarterRoutineResponse{
		Morning:            s.Morning,
		Evening:            s.Evening,
		WeekNotes:          s.WeekNotes,
		SafetyNotes:        s.SafetyNotes,
		Encouragement:      s.Encouragement,
		SkinReadback:       s.SkinReadback,
		Rationale:          s.Rationale,
		ClosingReminder:    s.ClosingReminder,
		ProductSuggestions: s.ProductSuggestions,
	}
}

// quickStarterFromOnboarding builds an immediate AM/PM scaffold from form
// answers so CompleteOnboarding can respond without waiting on the LLM.
func quickStarterFromOnboarding(req dto.OnboardingCompleteRequest, locale string) ai.StarterRoutine {
	bullets := buildStarterPackBullets(req)
	isEn := locale == "en"

	morning := []string{
		ternary(isEn,
			"Gentle cleanser — soft wash, no hard scrubbing.",
			"Rửa mặt dịu — làm sạch nhẹ, không chà mạnh.",
		),
		ternary(isEn,
			"Light moisturizer — keep skin comfortable.",
			"Kem dưỡng ẩm nhẹ — giữ da êm trong ngày.",
		),
		ternary(isEn,
			"Morning sunscreen — protect even near windows at home.",
			"Kem chống nắng buổi sáng — bảo vệ kể cả ở nhà gần cửa sổ.",
		),
	}
	evening := []string{
		ternary(isEn,
			"Evening cleanse — remove the day gently.",
			"Rửa mặt buổi tối — gỡ nhẹ bụi và sản phẩm trong ngày.",
		),
		ternary(isEn,
			"Light moisturizer — overnight comfort.",
			"Kem dưỡng ẩm nhẹ — đủ êm qua đêm.",
		),
		ternary(isEn,
			"Add one active only when skin feels calm — not every night at first.",
			"Chỉ thêm 1 hoạt chất khi da đã êm — chưa cần dùng mỗi tối ngay.",
		),
	}
	if len(bullets) > 0 {
		morning = bullets[:minInt(3, len(bullets))]
		if len(bullets) > 3 {
			evening = bullets[3:minInt(6, len(bullets))]
		}
	}

	var encouragement, weekNotes, safetyNotes, closing string
	if isEn {
		encouragement = "You finished getting-to-know-your-skin — nice work taking that first step."
		weekNotes = ""
		safetyNotes = "General skincare guidance only — not a substitute for medical advice."
		closing = "Track gently day by day — see a dermatologist when something worries you."
	} else {
		encouragement = "Bạn vừa hoàn thành phần làm quen với da — bước đầu rất đáng khen."
		weekNotes = ""
		safetyNotes = "Chỉ là gợi ý chăm sóc da chung — không thay thế tư vấn y tế."
		closing = "Theo dõi nhẹ nhàng từng ngày — hỏi bác sĩ da liễu khi bạn lo lắng."
	}

	return ai.StarterRoutine{
		Morning:         morning,
		Evening:         evening,
		WeekNotes:       weekNotes,
		SafetyNotes:     safetyNotes,
		Encouragement:   encouragement,
		Rationale:       "",
		ClosingReminder: closing,
	}
}

func buildStarterPackBullets(req dto.OnboardingCompleteRequest) []string {
	skill := strings.ToLower(strings.TrimSpace(req.SkillLevel))
	lines := make([]string, 0, 4)

	switch skill {
	case "beginner":
		lines = append(lines,
			"Rửa mặt dịu — nền an toàn mỗi sáng trước kem dưỡng và kem chống nắng.",
			"Kem chống nắng buổi sáng — kể cả ở nhà gần cửa sổ.",
			"Mỗi tuần chỉ thêm tối đa 1 sản phẩm mới — thử ít ở vùng nhỏ trước.",
		)
	case "intermediate":
		lines = append(lines,
			"Nếu dùng dung dịch tẩy da chết nhẹ vào tối — luôn kèm kem dưỡng khi da căng.",
			"Ghi routine 5–7 ngày để nhìn da thay đổi, không đổi nhiều sản phẩm cùng lúc.",
		)
	case "advanced":
		lines = append(lines,
			"Xếp lớp sản phẩm có chủ đích; đi chậm với acid/retinol và ghi cảm giác da hôm sau.",
			"So ảnh cùng ánh sáng/góc trước khi kết luận da đang khá hơn.",
		)
	}

	goal := strings.TrimSpace(req.Goal)
	if goal != "" && goal != "unsure" {
		goalLabel := mapQuickGoalLabel(goal)
		lines = append(lines, fmt.Sprintf(
			"Mục tiêu: %s — ưu tiên giải thích “vì sao” trước “dùng gì”.",
			goalLabel,
		))
	}
	if len(lines) > 6 {
		lines = lines[:6]
	}
	return lines
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func mapQuickGoalLabel(goal string) string {
	switch strings.ToLower(strings.TrimSpace(goal)) {
	case "glow":
		return "da sáng khoẻ"
	case "clear_acne":
		return "giảm mụn"
	case "barrier":
		return "làm dịu / da dễ kích ứng"
	case "anti_aging":
		return "căng ẩm / chống lão hoá nhẹ"
	default:
		return goal
	}
}
