package profile

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/service/ai"
)

// StarterReplayBudget caps one starter generation. It is shorter than
// repository.StarterClaimStaleAge (5m) so a stale worker cannot still be
// writing when the lease is reclaimed.
const StarterReplayBudget = 4 * time.Minute

// starterRoutineBGTimeout caps the background LLM job that personalizes the
// starter routine after CompleteOnboarding returns the quick scaffold.
const starterRoutineBGTimeout = StarterReplayBudget

// StarterInFlightGrace is how long a starter_routine_pending flag is treated
// as a live background job. Older pending rows are replay candidates.
const StarterInFlightGrace = 5 * time.Minute

// QuickScaffoldEncouragementVI and QuickScaffoldEncouragementEN are the fixed
// first lines of the local scaffold. A persisted onboarding snapshot that still
// carries one of them never received the AI starter routine.
const (
	QuickScaffoldEncouragementVI = "Bạn vừa hoàn thành phần làm quen với da — bước đầu rất đáng khen."
	QuickScaffoldEncouragementEN = "You finished getting-to-know-your-skin — nice work taking that first step."
)

// StarterRoutineSourceAI marks onboarding_snapshot.starter_routine_source after
// a successful model routine replaces the scaffold.
const StarterRoutineSourceAI = "ai"

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
		ProductGuidance:    s.ProductGuidance,
	}
}

const maxClientStarterSteps = 12

// Caps for client-supplied coach copy (guest claim / edited starter).
const (
	maxClientCoachCopyShort = 500
	maxClientCoachCopyLong  = 4000
)

// sanitizeClientStarterSteps trims empty lines and caps list length.
func sanitizeClientStarterSteps(steps []string) []string {
	if len(steps) == 0 {
		return nil
	}
	out := make([]string, 0, len(steps))
	for _, raw := range steps {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		out = append(out, s)
		if len(out) >= maxClientStarterSteps {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyClientStarterSteps overlays user-edited AM/PM steps onto the scaffold.
// Returns true when the client sent at least one non-empty period.
func applyClientStarterSteps(starter *ai.StarterRoutine, req dto.OnboardingCompleteRequest) bool {
	if starter == nil {
		return false
	}
	morning := sanitizeClientStarterSteps(req.Morning)
	evening := sanitizeClientStarterSteps(req.Evening)
	if len(morning) == 0 && len(evening) == 0 {
		return false
	}
	if len(morning) > 0 {
		starter.Morning = morning
	}
	if len(evening) > 0 {
		starter.Evening = evening
	}
	applyClientStarterCopy(starter, req)
	return true
}

func sanitizeClientCoachCopy(s string, maxRunes int) string {
	v := strings.TrimSpace(s)
	if v == "" {
		return ""
	}
	if maxRunes <= 0 {
		return v
	}
	runes := []rune(v)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return v
}

// applyClientStarterCopy overlays optional personalized coach strings when the
// client locks AM/PM (guest claim / edited starter). Empty fields leave scaffold.
func applyClientStarterCopy(starter *ai.StarterRoutine, req dto.OnboardingCompleteRequest) {
	if starter == nil {
		return
	}
	if v := sanitizeClientCoachCopy(req.WeekNotes, maxClientCoachCopyLong); v != "" {
		starter.WeekNotes = v
	}
	if v := sanitizeClientCoachCopy(req.SafetyNotes, maxClientCoachCopyLong); v != "" {
		starter.SafetyNotes = v
	}
	if v := sanitizeClientCoachCopy(req.Encouragement, maxClientCoachCopyShort); v != "" {
		starter.Encouragement = v
	}
	if v := sanitizeClientCoachCopy(req.SkinReadback, maxClientCoachCopyLong); v != "" {
		starter.SkinReadback = v
	}
	if v := sanitizeClientCoachCopy(req.Rationale, maxClientCoachCopyLong); v != "" {
		starter.Rationale = v
	}
	if v := sanitizeClientCoachCopy(req.ClosingReminder, maxClientCoachCopyShort); v != "" {
		starter.ClosingReminder = v
	}
}

// quickStarterFromOnboarding builds an immediate AM/PM scaffold from form
// answers so CompleteOnboarding can respond without waiting on the LLM.
func quickStarterFromOnboarding(req dto.OnboardingCompleteRequest, locale string) ai.StarterRoutine {
	isEn := locale == "en"
	bullets := buildStarterPackBullets(req, locale)

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

	var encouragement, weekNotes, safetyNotes, closing string
	if isEn {
		encouragement = QuickScaffoldEncouragementEN
		weekNotes = strings.Join(bullets, "\n")
		safetyNotes = "General skincare guidance only — not a substitute for medical advice."
		closing = "Track gently day by day — see a dermatologist when something worries you."
	} else {
		encouragement = QuickScaffoldEncouragementVI
		weekNotes = strings.Join(bullets, "\n")
		safetyNotes = "Chỉ là gợi ý chăm sóc da chung — không thay thế tư vấn y tế."
		closing = "Theo dõi nhẹ nhàng từng ngày — hỏi bác sĩ da liễu khi bạn lo lắng."
	}

	out := ai.StarterRoutine{
		Morning:         morning,
		Evening:         evening,
		WeekNotes:       weekNotes,
		SafetyNotes:     safetyNotes,
		Encouragement:   encouragement,
		Rationale:       "",
		ClosingReminder: closing,
	}
	// Keep Step-1 analyze commerce so welcome matches the funnel (≤2 CTAs).
	if req.SkinAnalysis != nil {
		out.ProductGuidance = req.SkinAnalysis.ProductGuidance
		out.ProductSuggestions = req.SkinAnalysis.ProductSuggestions
	}
	return out
}

func buildStarterPackBullets(req dto.OnboardingCompleteRequest, locale string) []string {
	skill := strings.ToLower(strings.TrimSpace(req.SkillLevel))
	en := strings.EqualFold(locale, "en")
	lines := make([]string, 0, 4)

	switch skill {
	case "beginner":
		if en {
			lines = append(lines,
				"Gentle cleanse — a safe base before moisturizer and sunscreen.",
				"Morning sunscreen — even near windows at home.",
				"Add at most one new product per week — patch-test a small area first.",
			)
		} else {
			lines = append(lines,
				"Rửa mặt dịu — nền an toàn mỗi sáng trước kem dưỡng và kem chống nắng.",
				"Kem chống nắng buổi sáng — kể cả ở nhà gần cửa sổ.",
				"Mỗi tuần chỉ thêm tối đa 1 sản phẩm mới — thử ít ở vùng nhỏ trước.",
			)
		}
	case "intermediate":
		if en {
			lines = append(lines,
				"If you use a mild exfoliant at night — always follow with moisturizer when skin feels tight.",
				"Journal 5–7 days to see change; don’t swap many products at once.",
			)
		} else {
			lines = append(lines,
				"Nếu dùng dung dịch tẩy da chết nhẹ vào tối — luôn kèm kem dưỡng khi da căng.",
				"Ghi routine 5–7 ngày để nhìn da thay đổi, không đổi nhiều sản phẩm cùng lúc.",
			)
		}
	case "advanced":
		if en {
			lines = append(lines,
				"Layer with intent; go slow with acids/retinol and note how skin feels next day.",
				"Compare photos in the same light/angle before deciding skin is improving.",
			)
		} else {
			lines = append(lines,
				"Xếp lớp sản phẩm có chủ đích; đi chậm với acid/retinol và ghi cảm giác da hôm sau.",
				"So ảnh cùng ánh sáng/góc trước khi kết luận da đang khá hơn.",
			)
		}
	}

	goal := strings.TrimSpace(req.Goal)
	if goal != "" && goal != "unsure" {
		goalLabel := mapQuickGoalLabel(goal, locale)
		if en {
			lines = append(lines, fmt.Sprintf(
				"Goal: %s — prioritize “why” before “what to buy”.",
				goalLabel,
			))
		} else {
			lines = append(lines, fmt.Sprintf(
				"Mục tiêu: %s — ưu tiên giải thích “vì sao” trước “dùng gì”.",
				goalLabel,
			))
		}
	}
	if len(lines) > 6 {
		lines = lines[:6]
	}
	return lines
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// SnapshotNeedsAIStarter reports whether a persisted onboarding snapshot never
// received a model starter routine. User-edited AM/PM steps and rows already
// marked starter_routine_source=ai are left alone. Guest previews are not
// stored on skin_profiles and are ignored.
func SnapshotNeedsAIStarter(snapshot []byte) bool {
	snap := decodeSnapshotMap(snapshot)
	if snap == nil {
		return false
	}
	if via, _ := snap["completed_via"].(string); strings.TrimSpace(via) != "onboarding_v1" {
		return false
	}
	if snapshotBool(snap, "starter_user_edited") {
		return false
	}
	if src, _ := snap["starter_routine_source"].(string); src == StarterRoutineSourceAI {
		return false
	}
	if snapshotBool(snap, "starter_routine_pending") {
		return true
	}
	sr, ok := snap["starter_routine"]
	if !ok || sr == nil {
		return true
	}
	routine, ok := sr.(map[string]any)
	if !ok {
		return true
	}
	encouragement := strings.TrimSpace(stringField(routine, "encouragement"))
	if encouragement == QuickScaffoldEncouragementVI || encouragement == QuickScaffoldEncouragementEN {
		return true
	}
	if encouragement == "" && len(stringList(routine["morning"])) == 0 && len(stringList(routine["evening"])) == 0 {
		return true
	}
	return false
}

// StarterReplayCandidate is SnapshotNeedsAIStarter minus a starter refresh that
// is still inside StarterInFlightGrace (pending flag, profile updated recently).
func StarterReplayCandidate(snapshot []byte, updatedAt, now time.Time) bool {
	if !SnapshotNeedsAIStarter(snapshot) {
		return false
	}
	snap := decodeSnapshotMap(snapshot)
	if snapshotBool(snap, "starter_routine_pending") && !updatedAt.IsZero() && now.Sub(updatedAt) < StarterInFlightGrace {
		return false
	}
	return true
}

// starterPromptPayload is the onboarding JSON sent to the model. It drops the
// local scaffold and replay bookkeeping so the prompt matches the original
// CompleteOnboarding call.
func starterPromptPayload(snapshot []byte) ([]byte, error) {
	snap := decodeSnapshotMap(snapshot)
	if snap == nil {
		return []byte("{}"), nil
	}
	delete(snap, "starter_routine")
	delete(snap, "starter_routine_pending")
	delete(snap, "starter_routine_source")
	delete(snap, "starter_user_edited")
	return json.Marshal(snap)
}

func decodeSnapshotMap(snapshot []byte) map[string]any {
	if len(snapshot) == 0 {
		return nil
	}
	var snap map[string]any
	if err := json.Unmarshal(snapshot, &snap); err != nil || snap == nil {
		return nil
	}
	return snap
}

func snapshotBool(snap map[string]any, key string) bool {
	if snap == nil {
		return false
	}
	b, ok := snap[key].(bool)
	return ok && b
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func stringList(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func mapQuickGoalLabel(goal, locale string) string {
	en := strings.EqualFold(locale, "en")
	switch strings.ToLower(strings.TrimSpace(goal)) {
	case "glow":
		if en {
			return "healthy glow"
		}
		return "da sáng khoẻ"
	case "clear_acne":
		if en {
			return "clearer skin"
		}
		return "giảm mụn"
	case "barrier":
		if en {
			return "soothe easily irritated skin"
		}
		return "làm dịu / da dễ kích ứng"
	case "anti_aging":
		if en {
			return "plump / gentle anti-aging"
		}
		return "căng ẩm / chống lão hoá nhẹ"
	default:
		return goal
	}
}
