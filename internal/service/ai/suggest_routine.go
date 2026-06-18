// Package ai — suggest_routine.go produces an AM/PM skincare routine on demand
// from the user's SkinProfile + their most recent skin check. It is the engine
// behind the "AI gợi ý routine cho tôi hôm nay" button on /routine.
//
// Distinct from `starter_routine.go`:
//   - starter routine is generated ONCE at onboarding from raw form answers.
//   - suggest_routine is repeatable, reads from persisted profile + last check,
//     and supports a different language / skill mode per call.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

// SuggestRoutineInput is what the routine usecase hands to the AI layer.
type SuggestRoutineInput struct {
	Profile   *domain.SkinProfile
	LastCheck *domain.SkinCheck
	Locale    string // "vi" | "en"
	SkillMode string // "beginner" | "intermediate" | "advanced"
	FocusNote string // optional user hint for today (e.g. "da căng, dùng dịu")
	// UserMemory is the pre-formatted long-term memory block from
	// BuildUserMemoryContext (saved profile + recent check-ins with AI
	// summaries + thumbs-up/down votes + routine adherence). Empty string
	// is OK — the AI just runs without long-term personalisation.
	UserMemory string
}

// SuggestedRoutine is the model-side projection (later mapped to dto.SuggestRoutineResponse).
type SuggestedRoutine struct {
	Morning            []dto.RoutineStep       `json:"morning"`
	Evening            []dto.RoutineStep       `json:"evening"`
	Encouragement      string                  `json:"encouragement"`
	Rationale          string                  `json:"rationale"`
	FocusReason        string                  `json:"focus_reason"`
	WeekNotes          string                  `json:"week_notes"`
	SafetyNotes        string                  `json:"safety_notes"`
	ClosingReminder    string                  `json:"closing_reminder"`
	ProductSuggestions []dto.ProductSuggestion `json:"product_suggestions"`
}

// GenerateSuggestedRoutine calls Anthropic (preferred) or OpenAI to produce
// AM/PM routines tailored to the user's profile + last check-in. It does NOT
// persist anything — the handler returns the JSON straight to the client so
// the user can preview, tweak, then explicitly Save.
func GenerateSuggestedRoutine(ctx context.Context, cfg *config.Config, in SuggestRoutineInput) (SuggestedRoutine, error) {
	var zero SuggestedRoutine
	if cfg == nil {
		return zero, fmt.Errorf("ai suggest routine: config required")
	}

	if mem := strings.TrimSpace(in.UserMemory); mem != "" {
		slog.Debug(
			"routine-suggest: user_memory in prompt",
			"chars", len([]rune(mem)),
			"sections", strings.Join(inferSectionsFromText(mem), ","),
		)
	}

	locale := normalizeRoutineLocale(in.Locale)
	skill := normalizeRoutineSkillLevel(in.SkillMode, in.Profile)

	userMsg := buildSuggestRoutineUserMessage(in, locale, skill)
	systemMsg := suggestRoutineSystemPrompt()

	client := &http.Client{Timeout: 4 * time.Minute}

	result, err := TextCoachCompletion(ctx, cfg, client, "routine-suggest", systemMsg, userMsg)
	if err != nil {
		return zero, err
	}
	slog.Debug("routine suggest llm",
		"provider", result.Provider,
		"model", result.Model,
		"fallback", result.Fallback,
	)
	out, err := parseSuggestedRoutine(result.Text)
	if err == nil {
		out.ProductSuggestions = FinalizeProductSuggestions(out.ProductSuggestions, in.UserMemory)
		LogSuggestedRoutineOutput("", out)
	}
	return out, err
}

func parseSuggestedRoutine(text string) (SuggestedRoutine, error) {
	var zero SuggestedRoutine
	raw, err := ExtractJSONObject(text)
	if err != nil {
		return zero, err
	}
	// The AI may emit `morning` / `evening` either as objects (preferred) or
	// as a string array (legacy starter-routine shape). Try the rich shape
	// first, then fall back to a permissive decoder so a slightly-off model
	// reply still produces a usable routine.
	var rich SuggestedRoutine
	if err := json.Unmarshal(raw, &rich); err == nil && (len(rich.Morning) > 0 || len(rich.Evening) > 0) {
		normalizeSteps(rich.Morning)
		normalizeSteps(rich.Evening)
		if strings.TrimSpace(rich.Rationale) == "" {
			rich.Rationale = strings.TrimSpace(rich.FocusReason)
		}
		rich.ProductSuggestions = SanitizeProductSuggestions(rich.ProductSuggestions)
		return rich, nil
	}

	var loose struct {
		Morning            json.RawMessage         `json:"morning"`
		Evening            json.RawMessage         `json:"evening"`
		Encouragement      string                  `json:"encouragement"`
		Rationale          string                  `json:"rationale"`
		FocusReason        string                  `json:"focus_reason"`
		WeekNotes          string                  `json:"week_notes"`
		SafetyNotes        string                  `json:"safety_notes"`
		ClosingReminder    string                  `json:"closing_reminder"`
		ProductSuggestions []dto.ProductSuggestion `json:"product_suggestions"`
	}
	if err := json.Unmarshal(raw, &loose); err != nil {
		return zero, fmt.Errorf("ai suggest: parse json: %w", err)
	}
	rationale := loose.Rationale
	if strings.TrimSpace(rationale) == "" {
		rationale = strings.TrimSpace(loose.FocusReason)
	}
	return SuggestedRoutine{
		Morning:            coerceSteps(loose.Morning),
		Evening:            coerceSteps(loose.Evening),
		Encouragement:      loose.Encouragement,
		Rationale:          rationale,
		WeekNotes:          loose.WeekNotes,
		SafetyNotes:        loose.SafetyNotes,
		ClosingReminder:    loose.ClosingReminder,
		ProductSuggestions: SanitizeProductSuggestions(loose.ProductSuggestions),
	}, nil
}

// coerceSteps tolerates older "list of strings" replies and converts them
// into the canonical RoutineStep array.
func coerceSteps(raw json.RawMessage) []dto.RoutineStep {
	if len(raw) == 0 {
		return []dto.RoutineStep{}
	}
	var asObjects []dto.RoutineStep
	if err := json.Unmarshal(raw, &asObjects); err == nil && asObjects != nil {
		normalizeSteps(asObjects)
		return asObjects
	}
	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		out := make([]dto.RoutineStep, 0, len(asStrings))
		for _, s := range asStrings {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			out = append(out, dto.RoutineStep{
				Title:    s,
				Category: inferCategory(s),
			})
		}
		normalizeSteps(out)
		return out
	}
	return []dto.RoutineStep{}
}

// normalizeSteps ensures every step has a stable id and a category guess.
// We always re-derive the id even if the model sent one — keeps the JSON clean
// and avoids leaking model-side tokens into our schema.
func normalizeSteps(steps []dto.RoutineStep) {
	for i := range steps {
		steps[i].Title = strings.TrimSpace(steps[i].Title)
		steps[i].Notes = strings.TrimSpace(steps[i].Notes)
		steps[i].Category = strings.ToLower(strings.TrimSpace(steps[i].Category))
		if steps[i].Category == "" {
			steps[i].Category = inferCategory(steps[i].Title)
		}
		// Fresh id per call — frontend treats the response as a brand-new
		// "template" and assigns its own React keys anyway.
		steps[i].ID = newStepID()
		steps[i].Completed = false
	}
}

func newStepID() string {
	return uuid.New().String()
}

// inferCategory guesses a category bucket from a free-form step title so the
// UI can pick an icon. It's intentionally fuzzy: false negatives degrade to
// "other" which the UI handles. The keyword list is bilingual (vi/en).
func inferCategory(title string) string {
	t := strings.ToLower(title)
	switch {
	case containsAny(t, "spf", "kem chống nắng", "chống nắng", "sunscreen"):
		return "spf"
	case containsAny(t, "cleanser", "sữa rửa mặt", "rửa mặt", "tẩy trang", "cleansing oil"):
		return "cleanser"
	case containsAny(t, "toner", "nước hoa hồng", "essence"):
		return "toner"
	case containsAny(t, "serum", "ampoule"):
		return "serum"
	case containsAny(t, "moisturizer", "kem dưỡng", "lotion", "cream"):
		return "moisturizer"
	case containsAny(t, "retinol", "tretinoin", "bha", "aha", "vitamin c", "azelaic", "niacinamide"):
		return "treatment"
	case containsAny(t, "mask", "mặt nạ"):
		return "mask"
	case containsAny(t, "eye", "mắt"):
		return "eye"
	default:
		return "other"
	}
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func normalizeRoutineLocale(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "en":
		return "en"
	default:
		return "vi"
	}
}

// normalizeRoutineSkillLevel resolves the skill mode for the request. The
// frontend can override per-call (the user might want a "beginner" pass for
// a low-energy day) — fall back to the profile, then to "beginner" as the
// safest default.
func normalizeRoutineSkillLevel(raw string, p *domain.SkinProfile) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "beginner":
		return "beginner"
	case "intermediate":
		return "intermediate"
	case "advanced":
		return "advanced"
	}
	if p != nil {
		switch p.SkillLevel {
		case domain.SkillLevelAdvanced:
			return "advanced"
		case domain.SkillLevelIntermediate:
			return "intermediate"
		case domain.SkillLevelBeginner:
			return "beginner"
		}
	}
	return "beginner"
}

func buildSuggestRoutineUserMessage(in SuggestRoutineInput, locale, skill string) string {
	var b strings.Builder
	if locale == "vi" {
		b.WriteString("Ngôn ngữ đầu ra: Tiếng Việt thân thiện, dễ hiểu. JSON key giữ tiếng Anh.\n\n")
	} else {
		b.WriteString("Output language: friendly English. JSON keys stay English.\n\n")
	}
	b.WriteString("Skill level: " + skill + "\n\n")

	b.WriteString("SKIN_PROFILE:\n")
	b.WriteString(BuildSkinProfileContext(in.Profile))

	if in.LastCheck != nil {
		b.WriteString("\nLATEST_CHECK_IN:\n")
		b.WriteString(BuildCheckInContext(in.LastCheck))
	} else {
		b.WriteString("\nLATEST_CHECK_IN: (none — use profile only)\n")
	}

	if note := strings.TrimSpace(in.FocusNote); note != "" {
		b.WriteString("\nFOCUS_NOTE: ")
		b.WriteString(note)
		b.WriteString("\n")
	}

	if mem := strings.TrimSpace(in.UserMemory); mem != "" {
		b.WriteString("\n")
		b.WriteString(mem)
	}

	b.WriteString("\nJSON schema reminder:\n")
	b.WriteString(`{"morning":["short step"],"evening":["short step"],"encouragement":"...","safety_notes":"...","focus_reason":"... (optional)",` + ProductSuggestionsJSONField + `}`)
	AppendAffiliateCoachContext(&b)
	return b.String()
}

// suggestRoutineSystemPrompt — token-minimal, tuned for fast models (Haiku / 4o-mini).
func suggestRoutineSystemPrompt() string {
	return `Bạn là DaDiary AI Coach — thân thiện và ngắn gọn.

Tạo routine sáng/tối dựa trên profile, check-in gần nhất, focus note và USER_MEMORY.

**Quy tắc:**
- Tối đa 4 bước sáng và 4 bước tối. Mỗi bước chỉ 3–8 từ.
- Buổi sáng bắt buộc có kem chống nắng.
- Buổi tối không có kem chống nắng.
- Beginner: chỉ 2–3 bước dịu nhẹ, tránh hoạt chất mạnh.
- Da nhạy cảm, đỏ, châm chích hoặc kích ứng gần đây → ưu tiên dưỡng ẩm và làm dịu, bỏ BHA/AHA/retinol/Vitamin C.
- Adherence thấp → giảm còn 2–3 bước mỗi buổi.
- Tôn trọng feedback 👎 trước đây của user.
- Intermediate/Advanced: tối đa 1 bước điều trị nếu phù hợp.

**Output (JSON thuần):**
{
  "morning": ["tên bước ngắn"],
  "evening": ["tên bước ngắn"],
  "encouragement": "Câu ngắn khích lệ",
  "safety_notes": "Lưu ý an toàn ngắn",
  "focus_reason": "Lý do chọn routine này (nếu cần)",
  "product_suggestions": []
}

Tên bước dùng vai trò chung (ví dụ: "gentle cleanser"). Chỉ gợi ý sản phẩm cụ thể trong product_suggestions (AFFILIATE_CATALOG trong user message). Không markdown, không text ngoài JSON.`
}
