package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

// BuildCheckInContext formats self-report & environment lines for the coach (shared by Claude and GPT fallback).
// Optional JSON field coach_skill_level inside ClimateContext is sent by the web app so the coach matches
// the user's selected Beginner / Intermediate / Advanced toggle for this session.
func BuildCheckInContext(o *domain.SkinCheck) string {
	if o == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Daily skin check-in")
	if strings.TrimSpace(o.Title) != "" {
		fmt.Fprintf(&b, ": %s", strings.TrimSpace(o.Title))
	}
	b.WriteString(".\n")
	if strings.TrimSpace(o.UserNote) != "" {
		fmt.Fprintf(&b, "User note: %s\n", strings.TrimSpace(o.UserNote))
	}
	conds, _ := dto.DecodeStringSlice(o.Conditions)
	if len(conds) > 0 {
		fmt.Fprintf(&b, "Self-reported conditions/tags: %s\n", strings.Join(conds, ", "))
	}
	syms, _ := dto.DecodeStringSlice(o.Symptoms)
	if len(syms) > 0 {
		fmt.Fprintf(&b, "Self-reported symptoms: %s\n", strings.Join(syms, ", "))
	}
	if strings.TrimSpace(o.EnvironmentNote) != "" {
		fmt.Fprintf(&b, "Context (weather/sleep/stress): %s\n", strings.TrimSpace(o.EnvironmentNote))
	}
	if len(o.ClimateContext) > 0 {
		var m map[string]any
		if err := json.Unmarshal(o.ClimateContext, &m); err == nil && len(m) > 0 {
			if v, ok := m["coach_skill_level"].(string); ok && strings.TrimSpace(v) != "" {
				fmt.Fprintf(&b, "Coaching depth for this session (user picked in app): %s\n", strings.TrimSpace(v))
			}
			rest := make(map[string]any, len(m))
			for k, val := range m {
				if k != "coach_skill_level" && k != "ui_locale" {
					rest[k] = val
				}
			}
			if len(rest) > 0 {
				rawRest, err := json.Marshal(rest)
				if err == nil {
					b.WriteString("Extra app / climate context (JSON): ")
					b.Write(rawRest)
					b.WriteString("\n")
				}
			}
		} else {
			b.WriteString("Climate snapshot (JSON): ")
			b.Write(o.ClimateContext)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// BuildSkinProfileContext summarizes the user’s saved SkinProfile for coach personalization (undertone, goals from onboarding snapshot).
// Pass nil if the user has no profile row — coach can still rely on vision + today’s check-in only.
func BuildSkinProfileContext(p *domain.SkinProfile) string {
	if p == nil {
		return "No saved skin profile yet — personalize using today’s check-in and photo cues only.\n"
	}
	var b strings.Builder
	if strings.TrimSpace(p.SkinType) != "" {
		fmt.Fprintf(&b, "- On-file skin type (from profile / onboarding): %s\n", strings.TrimSpace(p.SkinType))
	}
	if p.SkillLevel != "" && p.SkillLevel != domain.SkillLevelUnspecified {
		fmt.Fprintf(&b, "- Preferred coaching depth: %s\n", p.SkillLevel)
	}
	concerns, _ := dto.DecodeStringSlice(p.Concerns)
	if len(concerns) > 0 {
		fmt.Fprintf(&b, "- Tags / concerns on profile: %s\n", strings.Join(concerns, ", "))
	}
	if strings.TrimSpace(p.Notes) != "" {
		fmt.Fprintf(&b, "- Profile notes: %s\n", strings.TrimSpace(p.Notes))
	}
	if len(p.OnboardingSnapshot) > 0 {
		var snap map[string]any
		if err := json.Unmarshal(p.OnboardingSnapshot, &snap); err == nil && snap != nil {
			if u, ok := snap["undertone"].(string); ok && strings.TrimSpace(u) != "" {
				fmt.Fprintf(&b, "- Undertone (onboarding): %s\n", strings.TrimSpace(u))
			}
			if g, ok := snap["goal"].(string); ok && strings.TrimSpace(g) != "" {
				fmt.Fprintf(&b, "- Primary goal (onboarding): %s\n", strings.TrimSpace(g))
			}
			if raw, ok := snap["body_concerns"].([]any); ok {
				parts := make([]string, 0, len(raw))
				for _, x := range raw {
					if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
						parts = append(parts, strings.TrimSpace(s))
					}
				}
				if len(parts) > 0 {
					fmt.Fprintf(&b, "- Concerns captured at onboarding: %s\n", strings.Join(parts, ", "))
				}
			}
		}
	}
	if strings.TrimSpace(p.HomeCountryCode) != "" {
		fmt.Fprintf(&b, "- Home country hint (ISO-2): %s\n", strings.TrimSpace(p.HomeCountryCode))
	}
	if b.Len() == 0 {
		return "Profile row exists but has sparse fields — rely on today’s check-in for detail.\n"
	}
	return b.String()
}

// coachUserInterfaceLocaleDirective reads climate_context.ui_locale from the client (e.g. next-intl route).
// When set, the coach MUST write all JSON string fields in that language so the UI matches the user’s app language.
func coachUserInterfaceLocaleDirective(check *domain.SkinCheck) string {
	if check == nil || len(check.ClimateContext) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(check.ClimateContext, &m); err != nil || m == nil {
		return ""
	}
	v, ok := m["ui_locale"].(string)
	if !ok {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "vi":
		return "USER_INTERFACE_LOCALE: vi — The DaDiary app UI is Vietnamese for a friendly, beginner-leaning audience. Write **every** human-readable string in your JSON (strengths, situation_analysis, improvements.tip/why, routine_hints, avoid_or_patch, safety_reminders, medical_disclaimer, concern_alignment, summary_notes) in **natural, warm, conversational Vietnamese**. Prefer everyday words: 'lớp bảo vệ da' (instead of 'barrier'), 'kem chống nắng' (instead of 'SPF' alone), 'da khô bên trong' (instead of 'dehydrated'), 'da dễ nổi mụn' (instead of 'acne-prone'), 'thử trước trên vùng da nhỏ' (instead of 'patch test'), 'tẩy da chết' (instead of 'exfoliant'). When you must use a technical term, add a short Vietnamese explanation in parentheses on first mention. JSON keys stay English."
	case "en":
		return "USER_INTERFACE_LOCALE: en — The app UI is English. Write **every** human-readable string in your JSON in **natural, warm English** that a beginner could follow. Prefer 'sunscreen' over 'SPF' alone, 'skin barrier' over 'barrier' on first mention, and explain technical ingredients briefly the first time. JSON keys stay English."
	default:
		return ""
	}
}

// BuildDailyCheckInCoachContext joins long-term profile context with today’s self-report for Claude / GPT coach calls.
func BuildDailyCheckInCoachContext(check *domain.SkinCheck, profile *domain.SkinProfile) string {
	var out strings.Builder
	if line := coachUserInterfaceLocaleDirective(check); line != "" {
		out.WriteString(line)
		out.WriteString("\n")
	}
	out.WriteString("SKIN_PROFILE_CONTEXT (persistent — use to personalize tone, routineHints, and concern_alignment; if it conflicts with TODAY’s self-report, trust today and note the change gently):\n")
	out.WriteString(BuildSkinProfileContext(profile))
	out.WriteString("\nTODAY_CHECK_IN (this session):\n")
	out.WriteString(BuildCheckInContext(check))
	return out.String()
}

// BuildRecentCheckInsContext formats prior diary rows for the coach (token-light). Pass nil/empty if none.
func BuildRecentCheckInsContext(recent []domain.SkinCheck) string {
	if len(recent) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("RECENT_DIARY (older check-ins — same user; use for trends, consistency praise, and spotting improvement. If any line conflicts with TODAY_CHECK_IN or the photo, prioritize TODAY and acknowledge change supportively):\n")
	for _, c := range recent {
		dateStr := c.CheckDate.UTC().Format("2006-01-02")
		conds, _ := dto.DecodeStringSlice(c.Conditions)
		syms, _ := dto.DecodeStringSlice(c.Symptoms)
		fmt.Fprintf(&b, "- %s", dateStr)
		if len(conds) > 0 {
			fmt.Fprintf(&b, " | tags: %s", strings.Join(conds, ", "))
		}
		if len(syms) > 0 {
			fmt.Fprintf(&b, " | signals: %s", strings.Join(syms, ", "))
		}
		if t := strings.TrimSpace(c.Title); t != "" {
			fmt.Fprintf(&b, " | title: %s", truncateRunes(t, 80))
		}
		if note := strings.TrimSpace(c.UserNote); note != "" {
			fmt.Fprintf(&b, " | note: %s", truncateRunes(note, 140))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "…"
}

// BuildPriorFeedbackContext renders the user's most recent thumbs-up/down
// votes (plus optional reasons) as a compact prompt block so the coach can
// adapt tone / suggestion style over time.
//
// The block is intentionally short (we only quote up to the most recent ~12
// rows, with each comment trimmed) so it does not crowd out today's check-in
// signal. When `feedback` is empty the function returns "" — callers can
// safely concatenate it without guarding for nil.
//
// Format example (Vietnamese coach prompt expects this):
//
//   USER_FEEDBACK_HISTORY (most recent first — adjust tone & suggestions accordingly):
//     - 2026-05-12 | suggested_routine | 👎 | "BHA quá mạnh, da em nhạy"
//     - 2026-05-10 | skin_analysis     | 👍
//     - ...
//   GUIDANCE: Repeat patterns the user marked 👍 (warm, gentle, low-active).
//             Avoid patterns the user marked 👎 (do NOT push the same routine
//             angle again without acknowledging the prior feedback).
func BuildPriorFeedbackContext(feedback []domain.AIUserFeedback) string {
	if len(feedback) == 0 {
		return ""
	}
	const maxRows = 6

	rows := feedback
	if len(rows) > maxRows {
		rows = rows[:maxRows]
	}

	var helpfulCount, notHelpfulCount int
	for _, r := range rows {
		switch r.Rating {
		case string(domain.AIFeedbackHelpful):
			helpfulCount++
		case string(domain.AIFeedbackNotHelpful):
			notHelpfulCount++
		}
	}

	var b strings.Builder
	b.WriteString("USER_FEEDBACK_HISTORY (most recent first — same user; adjust tone & specifics so future suggestions feel more 'đúng gu' to this person):\n")
	for _, r := range rows {
		date := r.CreatedAt.UTC().Format("2006-01-02")
		if r.CreatedAt.IsZero() {
			date = time.Now().UTC().Format("2006-01-02")
		}
		emoji := "·"
		switch r.Rating {
		case string(domain.AIFeedbackHelpful):
			emoji = "👍 helpful"
		case string(domain.AIFeedbackNotHelpful):
			emoji = "👎 not_helpful"
		}
		target := strings.TrimSpace(r.TargetType)
		if target == "" {
			target = "ai_output"
		}
		fmt.Fprintf(&b, "  - %s | %s | %s", date, target, emoji)
		if c := strings.TrimSpace(r.Comment); c != "" {
			fmt.Fprintf(&b, " | reason: %q", truncateRunes(c, 200))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b,
		"  (totals in window: 👍 %d / 👎 %d)\n",
		helpfulCount, notHelpfulCount,
	)
	b.WriteString("GUIDANCE: Repeat patterns + tone the user marked 👍 (their preferred coaching voice). For 👎 entries, change angle: do NOT push the same suggestion or framing again. If reasons are present, address them implicitly (e.g. user said 'quá mạnh' → propose gentler alternatives this turn). Never quote the user's reason verbatim — paraphrase warmly.\n")
	return b.String()
}
