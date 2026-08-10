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
	Morning            []string                   `json:"morning"`
	Evening            []string                   `json:"evening"`
	WeekNotes          string                     `json:"week_notes"`
	SafetyNotes        string                     `json:"safety_notes"`
	Encouragement      string                     `json:"encouragement"`
	SkinReadback       string                     `json:"skin_readback"`
	Rationale          string                     `json:"rationale"`
	ClosingReminder    string                     `json:"closing_reminder"`
	ProductSuggestions []dto.ProductSuggestion    `json:"product_suggestions"`
	ProductGuidance    []dto.ProductGuidanceItem  `json:"product_guidance,omitempty"`
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
	langLine := "Output language: **friendly, beginner English**. Every user-facing string must be plain and warm. Prefer 'sunscreen' (not bare 'SPF'); say 'skin that gets irritated easily' instead of 'barrier'; each morning/evening step = familiar name + one short why; no % actives unless truly needed and explained simply; no diagnosis, no required brands."
	if strings.EqualFold(strings.TrimSpace(locale), "vi") {
		lang = "Vietnamese (Tiếng Việt)"
		langLine = "Ngôn ngữ đầu ra: **Tiếng Việt đời thường, ấm, rõ**. Không lẫn jargon Anh/y khoa. CẤM: barrier, erythema, sebum, papules, comedone, hyperpigmentation, inflammation, texture, T-zone, 'hàng rào da' (nói 'da dễ đỏ / da yếu hơn bình thường'). Dùng: nốt đỏ, mụn viêm, mụn ẩn, thâm, da bóng, da khô, lỗ chân lông to, kem chống nắng. Mỗi bước morning/evening = tên quen (rửa mặt, kem dưỡng, chống nắng…) + 1 câu vì sao ngắn. Không nhồi % hoạt chất trừ khi thật cần và giải thích dễ hiểu. Không chẩn đoán bệnh, không brand bắt buộc."
	}

	memoryBlock := ""
	if mem := strings.TrimSpace(userMemory); mem != "" {
		memoryBlock = `

This user has re-entered onboarding — they are NOT brand new. Use the long-term memory below for continuity. Fresh onboarding answers win on conflict; acknowledge gently in encouragement only (keep rationale and week_notes as "").

` + mem
	}

	return `Onboarding data (JSON):
` + payload + memoryBlock + `

` + langLine + `
The payload may contain English enum codes (e.g. goal, budget, undertone, skill_level). Interpret them; write all user-facing strings in ` + lang + `.
When **skin_analysis** is present (vision + coach photo pass), ground morning/evening steps and skin_readback on coaching_notes, detailed_observations, and main_concerns — keep region-specific cues from the photos.
` + affiliateStarterTail() + `
Trả về ONE JSON object duy nhất (không markdown), đúng cấu trúc sau:
{
  "encouragement": "Câu khích lệ kiểu bạn thân, có thể hơi xéo",
  "skin_readback": "Tóm tắt ngắn gọn loại da + concerns + mục tiêu",
  "morning": ["Bước 1", "Bước 2", "Bước 3"],
  "evening": ["Bước 1", "Bước 2", "Bước 3"],
  "rationale": "",
  "week_notes": "",
  "safety_notes": "Câu ngắn về an toàn (nếu cần)",
  "closing_reminder": "Câu nhắc nhở ngắn gọn",` + ProductSuggestionsJSONField + `
}
Quy tắc cứng: morning/evening tối đa 3 phần tử; rationale và week_notes luôn ""; product_suggestions tối đa 2 item từ AFFILIATE_CATALOG; mỗi reason = 1 câu ngắn về concern.
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
	if len(s.Morning) > 3 {
		s.Morning = s.Morning[:3]
	}
	if len(s.Evening) > 3 {
		s.Evening = s.Evening[:3]
	}
	s.Rationale = ""
	s.WeekNotes = ""
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
	attachStarterCommerce(&out, onboardingJSON, locale, userMemory)
	return out, nil
}

// attachStarterCommerce prefers Step-1 analyze product_guidance (funnel-stable ≤2 CTAs).
// Falls back to sanitized LLM/catalog picks mapped into the same guidance shape.
func attachStarterCommerce(out *StarterRoutine, onboardingJSON []byte, locale, userMemory string) {
	if out == nil {
		return
	}
	if g, phase := productGuidanceFromOnboardingJSON(onboardingJSON); len(g) > 0 {
		picks := suggestionsFromGuidance(g)
		picks = FinalizeProductSuggestionsLocale(picks, userMemory, locale)
		if phase == "" {
			phase = InferCarePhaseFromUserContext(string(onboardingJSON) + "\n" + userMemory)
		}
		out.ProductSuggestions = picks
		out.ProductGuidance = mergeGuidancePicks(g, picks, phase, locale)
		return
	}
	out.ProductSuggestions = FinalizeProductSuggestionsLocale(out.ProductSuggestions, userMemory, locale)
	if len(out.ProductSuggestions) == 0 {
		picks := PickStarterAffiliateSuggestions(onboardingJSON, locale)
		out.ProductSuggestions = FinalizeProductSuggestionsLocale(picks, userMemory, locale)
	}
	phase := InferCarePhaseFromUserContext(string(onboardingJSON) + "\n" + userMemory)
	out.ProductGuidance = ProductSuggestionsToGuidance(out.ProductSuggestions, phase, locale)
}

func productGuidanceFromOnboardingJSON(raw []byte) ([]dto.ProductGuidanceItem, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var snap struct {
		SkinAnalysis *struct {
			ProductGuidance []dto.ProductGuidanceItem `json:"product_guidance"`
			Phase           string                    `json:"phase"`
		} `json:"skin_analysis"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil || snap.SkinAnalysis == nil {
		return nil, ""
	}
	return snap.SkinAnalysis.ProductGuidance, strings.TrimSpace(snap.SkinAnalysis.Phase)
}

func suggestionsFromGuidance(items []dto.ProductGuidanceItem) []dto.ProductSuggestion {
	out := make([]dto.ProductSuggestion, 0, len(items))
	for _, g := range items {
		if strings.TrimSpace(g.AffiliateLink) == "" || strings.TrimSpace(g.ProductName) == "" {
			continue
		}
		out = append(out, dto.ProductSuggestion{
			ProductName:   g.ProductName,
			Brand:         g.Brand,
			Reason:        g.Why,
			AffiliateLink: g.AffiliateLink,
			PriceRange:    g.PriceRange,
			Priority:      "high",
			ProductID:     g.AffiliateProductID,
			Step:          g.Step,
			Benefits:      append([]string(nil), g.Benefits...),
			HowToUse:      g.HowToUse,
			Caution:       g.Caution,
		})
	}
	return out
}

// mergeGuidancePicks keeps Step-1 role cards; commerce fields only for sanitized picks (≤2).
func mergeGuidancePicks(
	templates []dto.ProductGuidanceItem,
	picks []dto.ProductSuggestion,
	phase, locale string,
) []dto.ProductGuidanceItem {
	byID := make(map[string]dto.ProductSuggestion, len(picks))
	byLink := make(map[string]dto.ProductSuggestion, len(picks))
	for _, p := range picks {
		if id := strings.TrimSpace(p.ProductID); id != "" {
			byID[id] = p
		}
		if link := normalizeAffiliateLink(p.AffiliateLink); link != "" {
			byLink[link] = p
		}
	}
	out := make([]dto.ProductGuidanceItem, 0, len(templates))
	used := 0
	for _, tmpl := range templates {
		item := tmpl
		item.Phase = phase
		item.AffiliateProductID = ""
		item.ProductName = ""
		item.Brand = ""
		item.AffiliateLink = ""
		item.PriceRange = ""
		if item.NameOrCategory == "" {
			item.NameOrCategory = genericRoleLabel(item.Step, item.Category, locale)
		}
		if used < maxProductSuggestions {
			var pick dto.ProductSuggestion
			ok := false
			if p, hit := byID[strings.TrimSpace(tmpl.AffiliateProductID)]; hit {
				pick, ok = p, true
			} else if p, hit := byLink[normalizeAffiliateLink(tmpl.AffiliateLink)]; hit {
				pick, ok = p, true
			}
			if ok {
				used++
				item.AffiliateProductID = pick.ProductID
				item.ProductName = pick.ProductName
				item.Brand = pick.Brand
				item.AffiliateLink = pick.AffiliateLink
				item.PriceRange = pick.PriceRange
				if strings.TrimSpace(item.Why) == "" {
					item.Why = pick.Reason
				}
			}
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return ProductSuggestionsToGuidance(picks, phase, locale)
	}
	return out
}
