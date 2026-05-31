// Package ai implements multi-provider coach calls; this file is onboarding "starter routine" text JSON.
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
	"github.com/dadiary/backend/internal/dto"
)

// StarterRoutine is the AM/PM scaffold and supportive coach copy returned to clients (API keys stable).
type StarterRoutine struct {
	Morning            []string                `json:"morning"`
	Evening            []string                `json:"evening"`
	WeekNotes          string                  `json:"week_notes"`
	SafetyNotes        string                  `json:"safety_notes"`
	Encouragement      string                  `json:"encouragement"`
	SkinReadback       string                  `json:"skin_readback"`
	Rationale          string                  `json:"rationale"`
	ClosingReminder    string                  `json:"closing_reminder"`
	ProductSuggestions []dto.ProductSuggestion `json:"product_suggestions"`
}

// starterUserMessage shapes the user turn for Claude / OpenAI JSON.
//
// userMemory is optional — when the caller is a returning user (re-onboarding,
// "redo my starter routine", etc.) it is the BuildUserMemoryContext block so
// the starter prompt can stay consistent with what the coach already told the
// user across past sessions. Empty string for first-time onboarders.
func starterUserMessage(onboardingJSON []byte, locale, userMemory string) string {
	payload := string(onboardingJSON)
	if strings.TrimSpace(payload) == "" {
		payload = "{}"
	}
	lang := "English"
	langLine := "Output language: **friendly, beginner-leaning English**. Write every user-facing string in natural, warm English a total beginner could follow. Prefer 'sunscreen' (not bare 'SPF'), 'skin barrier' on first mention, and explain any technical ingredient briefly."
	if strings.EqualFold(strings.TrimSpace(locale), "vi") {
		lang = "Vietnamese (Tiếng Việt)"
		langLine = "Ngôn ngữ đầu ra: **Tiếng Việt thân thiện, dễ hiểu**. Mọi chuỗi hiển thị cho người dùng phải bằng tiếng Việt đời thường (không lẫn câu tiếng Anh), giọng như một người bạn nói chuyện. Tránh thuật ngữ chuyên môn cứng (barrier → 'lớp bảo vệ da'; SPF → 'kem chống nắng'; dehydrated → 'da khô bên trong'; exfoliant → 'tẩy da chết'; patch test → 'thử trước trên vùng da nhỏ')."
	}

	memoryBlock := ""
	if mem := strings.TrimSpace(userMemory); mem != "" {
		memoryBlock = `

This user has re-entered onboarding — they are NOT brand new. Use the long-term memory below to keep continuity (skin type drift, prior preferences, votes). Treat the fresh onboarding answers above as authoritative for any conflict, but acknowledge the change gently in encouragement/rationale.

` + mem
	}

	return `Onboarding data (JSON):
` + payload + memoryBlock + `

` + langLine + `
The payload may contain English enum codes (e.g. goal, budget, undertone). Interpret them, but write **all** of encouragement, skin_readback, morning/evening steps, rationale, week_notes, safety_notes, and closing_reminder entirely in ` + lang + `.
` + affiliateStarterTail() + `
Respond with ONE JSON object exactly (all keys required; use "" or [] where there is nothing to say):
{
  "encouragement": "string",
  "skin_readback": "string",
  "morning": ["string", "..."],
  "evening": ["string", "..."],
  "rationale": "string",
  "week_notes": "string",
  "safety_notes": "string",
  "closing_reminder": "string",` + ProductSuggestionsJSONField + `
}
Output only this JSON object — no markdown fences, no extra text.
`
}

func affiliateStarterTail() string {
	var b strings.Builder
	AppendAffiliateCoachContext(&b)
	return b.String()
}

func normalizeStarterRoutine(s *StarterRoutine) {
	if s == nil {
		return
	}
	if s.Morning == nil {
		s.Morning = []string{}
	}
	if s.Evening == nil {
		s.Evening = []string{}
	}
	s.ProductSuggestions = SanitizeProductSuggestions(s.ProductSuggestions)
}

// GenerateStarterRoutine uses Anthropic when configured; otherwise OpenAI JSON chat (Model field).
// locale should be "vi" or "en" (UI locale); controls language of all string fields in the JSON result.
//
// userMemory is optional. Pass BuildUserMemoryContext output when the caller
// is a *returning* user (re-onboarding flow, profile-reset, etc.). Empty
// string is the right default for true first-time onboarders.
func GenerateStarterRoutine(ctx context.Context, cfg *config.Config, onboardingJSON []byte, locale, userMemory string) (StarterRoutine, error) {
	var zero StarterRoutine
	if cfg == nil {
		return zero, fmt.Errorf("ai starter: config required")
	}
	client := &http.Client{Timeout: 4 * time.Minute}
	userMsg := starterUserMessage(onboardingJSON, locale, userMemory)

	if mem := strings.TrimSpace(userMemory); mem != "" {
		slog.Debug(
			"starter-routine: user_memory in prompt",
			"chars", len([]rune(mem)),
			"sections", strings.Join(inferSectionsFromText(mem), ","),
		)
	}

	// Hybrid: Claude Sonnet primary; GPT-4o text fallback on missing key or Claude error.
	result, err := TextCoachCompletion(ctx, cfg, client, "starter-routine", StarterRoutineSystemPrompt(), userMsg)
	if err != nil {
		return zero, err
	}
	slog.Debug("starter routine llm",
		"provider", result.Provider,
		"model", result.Model,
		"fallback", result.Fallback,
	)
	raw, err := ExtractJSONObject(result.Text)
	if err != nil {
		return zero, err
	}
	var out StarterRoutine
	if err := json.Unmarshal(raw, &out); err != nil {
		return zero, fmt.Errorf("ai starter: parse json: %w", err)
	}
	normalizeStarterRoutine(&out)
	out.ProductSuggestions = FinalizeProductSuggestions(out.ProductSuggestions, userMemory)
	return out, nil
}
