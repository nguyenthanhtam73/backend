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
			"Gentle cleanser + moisturizer + SPF in the morning.",
			"Sáng: sữa rửa mặt dịu + kem dưỡng ẩm + kem chống nắng.",
		),
	}
	evening := []string{
		ternary(isEn,
			"Evening: cleanse + light moisturizer; add one active only when skin feels calm.",
			"Tối: rửa mặt + dưỡng ẩm nhẹ; thêm hoạt chất khi da ổn định.",
		),
	}
	if len(bullets) > 0 {
		morning = bullets[:minInt(2, len(bullets))]
		if len(bullets) > 2 {
			end := minInt(4, len(bullets))
			evening = bullets[2:end]
		}
	}

	var encouragement, weekNotes, safetyNotes, closing string
	if isEn {
		encouragement = "You finished getting-to-know-your-skin — nice work taking that first step."
		weekNotes = "Your personalized routine is being refined — this page will update automatically."
		safetyNotes = "General skincare guidance only — not a substitute for medical advice."
		closing = "Track gently day by day — see a dermatologist when something worries you."
	} else {
		encouragement = "Bạn vừa hoàn thành phần làm quen với da — bước đầu rất đáng khen."
		weekNotes = "Coach đang hoàn thiện routine cá nhân hóa — trang sẽ tự cập nhật trong giây lát."
		safetyNotes = "Chỉ là gợi ý chăm sóc da chung — không thay thế tư vấn y tế."
		closing = "Theo dõi nhẹ nhàng từng ngày — hỏi bác sĩ da liễu khi bạn lo lắng."
	}

	return ai.StarterRoutine{
		Morning:         morning,
		Evening:         evening,
		WeekNotes:       weekNotes,
		SafetyNotes:     safetyNotes,
		Encouragement:   encouragement,
		Rationale:       strings.Join(bullets, "\n"),
		ClosingReminder: closing,
	}
}

func buildStarterPackBullets(req dto.OnboardingCompleteRequest) []string {
	skill := strings.ToLower(strings.TrimSpace(req.SkillLevel))
	lines := make([]string, 0, 4)

	switch skill {
	case "beginner":
		lines = append(lines,
			"Nền an toàn: sữa rửa mặt dịu + kem dưỡng ẩm + SPF buổi sáng (kể cả ở nhà gần cửa sổ).",
			"Một hoạt chất mới / tuần — patch test trước khi full face.",
		)
	case "intermediate":
		lines = append(lines,
			"Xen kẽ hoạt chất (VD: BHA/PHA tối) — luôn kẹp cấp ẩm + phục hồi khi da căng.",
			"Ghi routine 5–7 ngày để nhìn pattern da, không đổi cùng lúc nhiều sản phẩm.",
		)
	case "advanced":
		lines = append(lines,
			"Tối ưu tầng (layering) có chủ đích; theo dõi pH và thứ tự acid/retinol.",
			"So ảnh cùng ánh sáng/góc trước khi kết luận “tiến triển”.",
		)
	}

	goal := strings.TrimSpace(req.Goal)
	if goal != "" && goal != "unsure" {
		lines = append(lines, fmt.Sprintf(
			"Mục tiêu: %s — coach AI sẽ ưu tiên giải thích “vì sao” trước “dùng gì”.",
			goal,
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
