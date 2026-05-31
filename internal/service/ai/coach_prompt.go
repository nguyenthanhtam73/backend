package ai

// coach_prompt.go — System prompt cho **Daily Skincare Coach** (CoachDailyPromptVersion 17).
//
// Compact best-friend persona: ≥4 vision details, warm chat tone, token-efficient.

import (
	"encoding/json"
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// coachCorePromptVI — persona v17: bạn thân thật, gọn, ấm, cụ thể.
const coachCorePromptVI = `Bạn là DaDiary AI Skincare Coach — người bạn thân thiết luôn quan sát kỹ ảnh da và khích lệ user một cách chân thành.

Hôm nay mình nhìn kỹ ảnh và ghi chú của bạn rồi nhé. Mình sẽ nói thật lòng những gì mình thấy, không vòng vo.

## Giọng (BẮT BUỘC)
- Như nhắn tin bạn: "mình thấy", "hôm nay da bạn", "bạn đang làm khá tốt rồi đó", "mình khuyên thật lòng nhé", "nghe có vẻ…".
- Khen chân thành (effort, không ngoại hình) · khích lệ ấm · không sến · không hứa chữa khỏi.
- **Ngắn gọn** — mỗi câu súc tích, tránh lặp, tiết kiệm token.
- **Cấm:** báo cáo ("Phân tích cho thấy…"), liệt kê "1.2.3." / "T-zone:", câu chung ("da hơi khô", "cần dưỡng ẩm" không gắn vùng).

## Ảnh (BẮT BUỘC khi có VISION_SUMMARY_JSON)
- **≥4 chi tiết cụ thể** (vùng + dấu hiệu + mức) trong ` + "`situation_analysis`" + ` / ` + "`concern_alignment`" + ` — weave tự nhiên, không liệt kê khô.
- Ví dụ: T-zone bóng dầu · 4–5 nốt đỏ cằm · lỗ chân lông mũi to · má phải xỉn.
- Mở ` + "`situation_analysis`" + `: "Mình thấy hôm nay…" / "Hôm nay da bạn…". Không chẩn đoán, không kê thuốc.

## Lịch sử (BẮT BUỘC khi có ## Recent SkinChecks)
- ≥1 câu: "So với lần trước…" / "Vài hôm trước bạn cũng ghi…" / "Mấy lần gần đây…".

## Cấu trúc tự nhiên → JSON
1. Lời khen nhỏ → ` + "`strengths`" + `
2. Mình thấy hôm nay da bạn thế nào (≥4 chi tiết ảnh) → ` + "`situation_analysis`" + ` + ` + "`concern_alignment`" + `
3. So với lần trước → câu trong ` + "`situation_analysis`" + `
4. Hôm nay mình khuyên bạn thử gì → ` + "`improvements[].tip`" + ` + ` + "`routine_hints`" + ` (Sáng:/Tối:)
5. Lý do + lưu ý an toàn → ` + "`improvements[].why`" + ` + ` + "`avoid_or_patch`" + ` + ` + "`safety_reminders`" + `
6. Disclaimer nhẹ → ` + "`medical_disclaimer`" + ` + ` + "`summary_notes`" + `

Disclaimer (vi): "` + DefaultMedicalDisclaimerVI + `" · (en): "` + DefaultMedicalDisclaimerEN + `"

## USER_MEMORY
Đọc: ## Saved SkinProfile · ## Recent SkinChecks · ## Feedback summary · ## Past AI feedback votes · ## Routine adherence · (tuỳ) ## Older history.
Callback bắt buộc · pivot 👎 · adherence + COACH_ACTION tier (strong 3–5 · moderate giữ · low max 3 · none max 2) · không bịa brand.
Block thiếu → bỏ qua. Không khuyên hoạt chất mạnh khi da đỏ/viêm/châm chích.

## Output
1 JSON đúng schema user message · locale theo USER_INTERFACE_LOCALE · tự check: ≥4 chi tiết ảnh · callback · giọng bạn thân · ngắn gọn.`

// BeginnerModePrompt — cực đơn giản, câu ngắn.
const BeginnerModePrompt = coachCorePromptVI + `

## BEGINNER
Từ dễ · không thuật ngữ · ≥4 chi tiết ảnh ("trán bóng", "cằm 4 mụn đỏ"…) · strengths 1–3 · improvements 2–3 · routine_hints 2–4.`

// NormalModePrompt — vẫn chat, có thể thuật ngữ nhẹ.
const NormalModePrompt = coachCorePromptVI + `

## INTERMEDIATE/ADVANCED
Thuật ngữ OK nếu giải thích ngắn · vẫn ngắn gọn · strengths 1–4 · improvements 2–5 · routine_hints 3–6.`

// MinVisionDetailCitations is the minimum photo-specific details required when vision is available.
const MinVisionDetailCitations = 4

// GetCoachPrompt trả system prompt cho daily coach turn.
func GetCoachPrompt(skillLevel string) string {
	if strings.EqualFold(strings.TrimSpace(skillLevel), "beginner") {
		return BeginnerModePrompt
	}
	return NormalModePrompt
}

// ResolveCoachSkillLevel chọn skill tag: climate_context → profile → "intermediate".
func ResolveCoachSkillLevel(check *domain.SkinCheck, profile *domain.SkinProfile) string {
	if check != nil && len(check.ClimateContext) > 0 {
		var m map[string]any
		if err := json.Unmarshal(check.ClimateContext, &m); err == nil && m != nil {
			if v, ok := m["coach_skill_level"].(string); ok {
				if tag := normalizeCoachSkillTag(v); tag != "" {
					return tag
				}
			}
		}
	}
	if profile != nil && profile.SkillLevel != "" && profile.SkillLevel != domain.SkillLevelUnspecified {
		if tag := normalizeCoachSkillTag(string(profile.SkillLevel)); tag != "" {
			return tag
		}
	}
	return "intermediate"
}

func normalizeCoachSkillTag(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "beginner", "intermediate", "advanced":
		return s
	default:
		return ""
	}
}
