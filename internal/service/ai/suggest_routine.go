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
	"github.com/dadiary/backend/internal/platform/openai"
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
	Morning         []dto.RoutineStep `json:"morning"`
	Evening         []dto.RoutineStep `json:"evening"`
	Encouragement   string            `json:"encouragement"`
	Rationale       string            `json:"rationale"`
	WeekNotes       string            `json:"week_notes"`
	SafetyNotes     string            `json:"safety_notes"`
	ClosingReminder string            `json:"closing_reminder"`
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

	if strings.TrimSpace(cfg.Anthropic.APIKey) != "" {
		text, err := AnthropicMessages(ctx, cfg, client, systemMsg, userMsg)
		if err != nil {
			return zero, err
		}
		out, err := parseSuggestedRoutine(text)
		if err == nil {
			LogSuggestedRoutineOutput("", out)
		}
		return out, err
	}

	text, err := openai.ChatCompletionJSON(ctx, cfg, client, systemMsg, userMsg)
	if err != nil {
		return zero, err
	}
	out, err := parseSuggestedRoutine(text)
	if err == nil {
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
		return rich, nil
	}

	var loose struct {
		Morning         json.RawMessage `json:"morning"`
		Evening         json.RawMessage `json:"evening"`
		Encouragement   string          `json:"encouragement"`
		Rationale       string          `json:"rationale"`
		WeekNotes       string          `json:"week_notes"`
		SafetyNotes     string          `json:"safety_notes"`
		ClosingReminder string          `json:"closing_reminder"`
	}
	if err := json.Unmarshal(raw, &loose); err != nil {
		return zero, fmt.Errorf("ai suggest: parse json: %w", err)
	}
	return SuggestedRoutine{
		Morning:         coerceSteps(loose.Morning),
		Evening:         coerceSteps(loose.Evening),
		Encouragement:   loose.Encouragement,
		Rationale:       loose.Rationale,
		WeekNotes:       loose.WeekNotes,
		SafetyNotes:     loose.SafetyNotes,
		ClosingReminder: loose.ClosingReminder,
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
	// Header instructions — language & shape, always first.
	if locale == "vi" {
		b.WriteString("Ngôn ngữ đầu ra: **Tiếng Việt thân thiện, dễ hiểu** (mọi chuỗi hiển thị cho người dùng). Dùng từ đời thường như một người bạn nói chuyện. Tránh thuật ngữ chuyên môn cứng (barrier → 'lớp bảo vệ da'; SPF → 'kem chống nắng'; dehydrated → 'da khô bên trong'; exfoliant → 'tẩy da chết'; patch test → 'thử trước trên vùng da nhỏ'). JSON key giữ tiếng Anh.\n\n")
	} else {
		b.WriteString("Output language: **friendly, beginner-leaning English** (all user-facing strings). Use everyday words; explain a technical ingredient briefly on first mention. JSON keys stay English.\n\n")
	}
	b.WriteString("Coaching depth requested: " + skill + "\n\n")

	b.WriteString("SKIN_PROFILE_CONTEXT:\n")
	b.WriteString(BuildSkinProfileContext(in.Profile))

	if in.LastCheck != nil {
		b.WriteString("\nMOST_RECENT_CHECK_IN:\n")
		b.WriteString(BuildCheckInContext(in.LastCheck))
	} else {
		b.WriteString("\nMOST_RECENT_CHECK_IN: (no recent check-in on file — rely on profile only.)\n")
	}

	if note := strings.TrimSpace(in.FocusNote); note != "" {
		b.WriteString("\nTODAY_FOCUS_FROM_USER: ")
		b.WriteString(note)
		b.WriteString("\n")
	}

	if mem := strings.TrimSpace(in.UserMemory); mem != "" {
		b.WriteString("\n")
		b.WriteString(mem)
	}

	b.WriteString("\nProduce ONE JSON object with this exact shape (do not output markdown, do not add commentary):\n")
	b.WriteString(`{
  "morning": [
    {"title": "string (one short, actionable line)", "category": "cleanser|toner|serum|moisturizer|spf|treatment|mask|eye|other", "notes": "string optional"}
  ],
  "evening": [
    {"title": "string", "category": "cleanser|toner|serum|moisturizer|treatment|mask|eye|other", "notes": "string optional"}
  ],
  "encouragement": "string — 1–2 supportive sentences, no diagnosis",
  "rationale": "string — short why-this-order in plain language",
  "week_notes": "string — what to expect / track this week",
  "safety_notes": "string — SPF, patch test, red-flag reminders",
  "closing_reminder": "string — gentle one-liner"
}`)
	b.WriteString("\n\nConstraints:\n")
	b.WriteString("- 3–5 steps each for AM and PM; beginners stay near 3, advanced may use 5.\n")
	b.WriteString("- AM must include SPF (or an equivalent leave-on photoprotection step).\n")
	b.WriteString("- Use generic product roles, no brand names unless the profile already names them.\n")
	b.WriteString("- Never claim diagnosis. Stay supportive, evidence-aware, non-pushy.\n")
	return b.String()
}

// suggestRoutineSystemPrompt is closely related to the starter prompt but is
// scoped for repeat use ("today's routine") and explicitly references the last
// check-in so the coach can adapt for flare-ups / sensitivity.
func suggestRoutineSystemPrompt() string {
	return `You are DaDiary’s friendly AI skincare buddy, producing a daily routine card for an existing user. Speak like a warm, encouraging friend — never a stern expert.

You receive: their saved SkinProfile (skin type, goal, concerns, skill level, region), their most recent daily skin check-in (tags, symptoms, photo context), an optional “today focus” free-text note, and a USER_MEMORY block (long-term history). Build a gentle, realistic morning/evening routine that they can actually follow today.

## Principles
- Not medical advice. Suggest seeing a dermatologist for severe / painful / rapidly worsening skin.
- Match skill level: **beginner (simple mode)** → 3 short steps, plain everyday words, NO strong active ingredients by default; **mid** → can mention one active with a short why; **advanced** → can layer / alternate actives.
- Morning always has sunscreen / photoprotection. Evening may include treatments (BHA, retinoid, etc.) only when appropriate to skill + tolerance signals from the check-in. If today’s tags signal irritation (sensitive, redness, weak_barrier, stinging), pause actives and pivot to soothing the skin barrier.
- Generic product **roles** — “gentle cleanser”, “hydrating toner”, “niacinamide serum” — never brand names unless the user already named brands in their profile.
- Keep every step under ~12 words. Frontend renders one bullet per step; long lines look bad on mobile.
- Output language is dictated by the user message (vi or en). Translate copy fully; JSON keys stay English.

## Personalising via USER_MEMORY (CRITICAL)
When USER_MEMORY is present (not "no saved memory yet"):
- rationale MUST reference at least one signal from Recent SkinChecks or Feedback summary.
- If 👎 reasons mention "quá mạnh" / "chung chung" / "quá nhiều bước" → adjust this routine (gentler, fewer steps, more specific step titles).
- Match routine step count to adherence tier (low/none → 2–3 steps per side max).
- Do NOT invent product names — generic roles only.

- **Recent SkinChecks** (5–8 latest):
  * If today's tags echo a recent pattern → write rationale to acknowledge continuity ("vài lần gần đây bạn cũng ghi da khô — routine hôm nay tiếp tục focus giữ ẩm").
  * If today is noticeably better → praise it in encouragement.
  * If today is worse → soften routine (fewer / gentler steps), say so in rationale.

- **Past AI feedback votes** (CRITICAL — never repeat an angle the user marked 👎):
  * 👎 with a stated reason (e.g. "BHA quá mạnh") → propose a clearly gentler alternative this turn; do NOT mention BHA. Do NOT quote the user's reason verbatim — paraphrase it implicitly.
  * 👎 with "too generic" → make this routine concrete: name the step type, the time of day, the why.
  * 👍 patterns (tone, structure) → keep them in encouragement / rationale this turn.

- **Routine adherence**:
  * tier=strong (≥75%) → 4–5 steps OK; can include one small upgrade; praise the consistency.
  * tier=moderate (40–74%) → keep the step count at or BELOW last time; rationale acknowledges effort without piling on.
  * tier=low (1–39%) → 3 steps each side max; rationale = "mình rút gọn nhẹ để bạn duy trì được"; zero guilt.
  * tier=none / no ticks → 2–3 steps total, ultra-low friction; encouragement = "thử quay lại một bước đơn giản tối nay" angle.

- **Older history** (>50 check-ins, monthly digest):
  * If a tag has shown up across multiple months → treat as chronic and pace the routine for the long run (not a 1-week fix).

When USER_MEMORY is empty / "no saved memory yet" → write rationale neutrally, no callbacks.

If today's check-in conflicts with USER_MEMORY (e.g. memory says "chronic dryness", today says "oily T-zone") → trust today and adapt the routine; acknowledge change gently in rationale.

## Vocabulary (Vietnamese — friendly, beginner-first)
- "lớp bảo vệ da" (instead of "barrier")
- "kem chống nắng" (instead of bare "SPF")
- "da khô bên trong" / "da thiếu nước" (instead of "dehydrated")
- "da dễ nổi mụn" (instead of "acne-prone")
- "thử trước trên vùng da nhỏ" (instead of "patch test")
- "tẩy da chết" (instead of "exfoliant")
- "thành phần đặc trị" / "hoạt chất" (instead of bare "active")
- For beginner mode (Vietnamese), avoid all jargon entirely or add a Vietnamese gloss in parentheses on first use.

## Output
ONE JSON object only, exactly matching the schema in the user message. No markdown fences, no commentary.`
}
