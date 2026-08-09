package dto

import (
	"encoding/json"
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// CreateSkinCheckResponse is returned after POST /api/v1/skin-checks succeeds.
// When AI runs synchronously, analysis.coach contains structured coach feedback (or error_message on pipeline failure).
type CreateSkinCheckResponse struct {
	Check     SkinCheckSummary     `json:"check"`
	Analysis  SkinAnalysisSummary  `json:"analysis"`
	ImageURLs []string             `json:"image_urls"`
	// Streak is set on create when the check-in updated the user's streak
	// (omitted on GET poll responses). Used so the client can toast auto-freeze.
	Streak *SkinCheckStreakMeta `json:"streak,omitempty"`
}

// SkinCheckStreakMeta summarizes streak side-effects of a successful check-in create.
//
// AutoFreezeApplied is true when the system spent one freeze to cover a single
// missed day (auto-freeze). Manual freezes are never applied here — those go
// through POST /me/streak/freeze.
type SkinCheckStreakMeta struct {
	AutoFreezeApplied   bool    `json:"auto_freeze_applied"`
	CatchUpContinued    bool    `json:"catch_up_continued,omitempty"`
	UnusedFreezeCleared bool    `json:"unused_freeze_cleared,omitempty"`
	CurrentStreak       int     `json:"current_streak"`
	FreezesAvailable    int     `json:"freezes_available"`
	ProtectedUntil      *string `json:"protected_until,omitempty"`
}

// SkinCheckSummary is a compact payload for API responses.
type SkinCheckSummary struct {
	ID              string          `json:"id"`
	UserID          string          `json:"user_id"`
	Title           string          `json:"title,omitempty"`
	UserNote        string          `json:"user_note,omitempty"`
	EnvironmentNote string          `json:"environment_note,omitempty"`
	Conditions      []string        `json:"conditions,omitempty"`
	Symptoms        []string        `json:"symptoms,omitempty"`
	ClimateContext  json.RawMessage `json:"climate_context,omitempty"`
	Visibility      string          `json:"visibility"`
	CheckDate       string          `json:"check_date"`
	CreatedAt       string          `json:"created_at"`
}

// CoachImprovementItem matches coach JSON improvements.{tip,why}.
type CoachImprovementItem struct {
	Tip string `json:"tip"`
	Why string `json:"why"`
}

// CoachCareSuggestionItem is an in-app detailed care step after check-in.
// Richer than public Admin Skin Review soothing_tips; never exposed on /share/skin-review.
//
// Example (in-app GET /skin-checks/:id → analysis.coach):
//
//	{
//	  "slot": "morning",
//	  "step": "Chống nắng",
//	  "why": "Má đang đỏ — SPF giúp hạn chế kích thêm khi ra nắng.",
//	  "safety_note": "Tránh nặn khi vùng viêm còn sưng."
//	}
type CoachCareSuggestionItem struct {
	Slot       string `json:"slot"`                  // morning | evening | today
	Step       string `json:"step"`                  // everyday step name
	Why        string `json:"why,omitempty"`         // why it fits today
	SafetyNote string `json:"safety_note,omitempty"` // optional caution
}

// SkinCoachDetail is structured AI feedback for the daily check-in UI (Claude primary pipeline).
//
// In-app care (detailed): care_suggestions + routine_hints + improvements + safety.
// Public share Admin Skin Review is a separate DTO (overview / possible_causes / soothing_tips only)
// and must not include care_suggestions or full AM–PM care checklists.
type SkinCoachDetail struct {
	SummaryNotes       string                     `json:"summary_notes,omitempty"`
	Strengths          []string                   `json:"strengths,omitempty"`
	SituationSummary   string                     `json:"situation_summary,omitempty"`
	ConcernAlignment   string                     `json:"concern_alignment,omitempty"`
	SkinScoreGauges    *SkinCoachScoreGauges      `json:"skin_score_gauges,omitempty"`
	Improvements       []CoachImprovementItem     `json:"improvements,omitempty"`
	CareSuggestions    []CoachCareSuggestionItem  `json:"care_suggestions,omitempty"`
	RoutineHints       []string                   `json:"routine_hints,omitempty"`
	AvoidOrPatch       []string                   `json:"avoid_or_patch,omitempty"`
	SafetyReminders    []string                   `json:"safety_reminders,omitempty"`
	MedicalDisclaimer  string                     `json:"medical_disclaimer,omitempty"`
	ProductSuggestions []ProductSuggestion        `json:"product_suggestions,omitempty"`
	ProductGuidance    []ProductGuidanceItem      `json:"product_guidance,omitempty"`
	CarePhase          string                     `json:"care_phase,omitempty"`
	ErrorMessage       string                     `json:"error_message,omitempty"`
}

// SkinCoachScoreGauges exposes soft 0–1 subscores from the coach JSON (not clinical).
type SkinCoachScoreGauges struct {
	Overall     *float64 `json:"overall,omitempty"`
	Hydration   *float64 `json:"hydration,omitempty"`
	Clarity     *float64 `json:"clarity,omitempty"`
	Barrier     *float64 `json:"barrier,omitempty"`
}

// SkinAnalysisSummary is the public read model for one AI analysis row.
type SkinAnalysisSummary struct {
	ID             string `json:"id"`
	SkinCheckID    string `json:"skin_check_id"`
	Status         string `json:"status"`
	ModelVersion   string `json:"model_version,omitempty"`
	PromptVersion  int    `json:"prompt_version,omitempty"`
	// Coach is set for completed (full detail) or failed (error_message only) after synchronous pipeline.
	Coach *SkinCoachDetail `json:"coach,omitempty"`
}

// NewCreateSkinCheckResponse builds API payload from domain rows plus public image paths.
func NewCreateSkinCheckResponse(c *domain.SkinCheck, a *domain.SkinAnalysis, publicImageURLs []string) CreateSkinCheckResponse {
	if c == nil {
		return CreateSkinCheckResponse{}
	}
	checkD := c.CheckDate.UTC().Format("2006-01-02")
	created := c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	conds, _ := DecodeStringSlice(c.Conditions)
	syms, _ := DecodeStringSlice(c.Symptoms)
	var climate json.RawMessage
	if len(c.ClimateContext) > 0 {
		climate = append(json.RawMessage(nil), c.ClimateContext...)
	}
	sum := SkinCheckSummary{
		ID:              c.ID.String(),
		UserID:          c.UserID.String(),
		Title:           c.Title,
		UserNote:        c.UserNote,
		EnvironmentNote: c.EnvironmentNote,
		Conditions:      conds,
		Symptoms:        syms,
		ClimateContext:  climate,
		Visibility:      string(c.Visibility),
		CheckDate:       checkD,
		CreatedAt:       created,
	}
	as := mapSkinAnalysisSummary(a)
	return CreateSkinCheckResponse{
		Check:     sum,
		Analysis:  as,
		ImageURLs: append([]string(nil), publicImageURLs...),
	}
}

// mapSkinAnalysisSummary maps DB JSON columns to a stable API shape for the mobile client.
func mapSkinAnalysisSummary(a *domain.SkinAnalysis) SkinAnalysisSummary {
	if a == nil {
		return SkinAnalysisSummary{}
	}
	out := SkinAnalysisSummary{
		ID:             a.ID.String(),
		SkinCheckID:    a.SkinCheckID.String(),
		Status:         string(a.Status),
		ModelVersion:   a.ModelVersion,
		PromptVersion:  a.PromptVersion,
	}
	switch a.Status {
	case domain.AnalysisStatusFailed:
		out.Coach = &SkinCoachDetail{
			ErrorMessage: strings.TrimSpace(a.ErrorMessage),
		}
	case domain.AnalysisStatusCompleted:
		out.Coach = buildCoachDetailFromDomain(a)
	default:
		// pending / processing — coach omitted (short window if client polls later)
	}
	return out
}

func buildCoachDetailFromDomain(a *domain.SkinAnalysis) *SkinCoachDetail {
	if a == nil {
		return nil
	}
	d := &SkinCoachDetail{
		SummaryNotes: a.SummaryNotes,
	}
	if len(a.Strengths) > 0 {
		_ = json.Unmarshal(a.Strengths, &d.Strengths)
	}
	if len(a.Improvements) > 0 {
		_ = json.Unmarshal(a.Improvements, &d.Improvements)
	}
	if len(a.RoutineHints) > 0 {
		_ = json.Unmarshal(a.RoutineHints, &d.RoutineHints)
	}
	if len(a.ProductSuggestions) > 0 {
		_ = json.Unmarshal(a.ProductSuggestions, &d.ProductSuggestions)
	}
	if len(a.AvoidOrPatch) > 0 {
		_ = json.Unmarshal(a.AvoidOrPatch, &d.AvoidOrPatch)
	}
	type safetyDTO struct {
		Reminders  []string `json:"reminders"`
		Disclaimer string   `json:"disclaimer"`
	}
	var sf safetyDTO
	if len(a.SafetyFlags) > 0 {
		_ = json.Unmarshal(a.SafetyFlags, &sf)
		d.SafetyReminders = sf.Reminders
		d.MedicalDisclaimer = sf.Disclaimer
	}
	if len(a.SkinScores) > 0 {
		var scores map[string]any
		if err := json.Unmarshal(a.SkinScores, &scores); err == nil && scores != nil {
			if v, ok := scores["situation_analysis"].(string); ok {
				d.SituationSummary = v
			}
			if v, ok := scores["concern_alignment"].(string); ok {
				d.ConcernAlignment = v
			}
			d.CareSuggestions = extractCareSuggestions(scores)
			g := extractScoreGauges(scores)
			if g != nil {
				d.SkinScoreGauges = g
			}
			if v, ok := scores["care_phase"].(string); ok {
				d.CarePhase = v
			}
			d.ProductGuidance = extractProductGuidance(scores)
		}
	}
	// Older analyses: synthesize checklist from improvements so FE still shows "Gợi ý chăm sóc".
	if len(d.CareSuggestions) == 0 && len(d.Improvements) > 0 {
		d.CareSuggestions = careSuggestionsFromImprovements(d.Improvements)
	}
	return d
}

// careSuggestionsFromImprovements maps legacy tip/why rows into care checklist items.
// Parses Sáng:/Tối:/AM:/PM: prefixes into morning/evening slots (same idea as AI normalize).
func careSuggestionsFromImprovements(imps []CoachImprovementItem) []CoachCareSuggestionItem {
	out := make([]CoachCareSuggestionItem, 0, len(imps))
	for _, imp := range imps {
		tip := strings.TrimSpace(imp.Tip)
		if tip == "" {
			continue
		}
		slot := "today"
		step := tip
		lower := strings.ToLower(tip)
		switch {
		case strings.HasPrefix(lower, "sáng:") || strings.HasPrefix(lower, "sang:") ||
			strings.HasPrefix(lower, "am:") || strings.HasPrefix(lower, "morning:"):
			slot = "morning"
			if i := strings.Index(tip, ":"); i >= 0 && i+1 < len(tip) {
				step = strings.TrimSpace(tip[i+1:])
			}
		case strings.HasPrefix(lower, "tối:") || strings.HasPrefix(lower, "toi:") ||
			strings.HasPrefix(lower, "pm:") || strings.HasPrefix(lower, "evening:"):
			slot = "evening"
			if i := strings.Index(tip, ":"); i >= 0 && i+1 < len(tip) {
				step = strings.TrimSpace(tip[i+1:])
			}
		}
		if step == "" {
			step = tip
		}
		out = append(out, CoachCareSuggestionItem{
			Slot: slot,
			Step: step,
			Why:  strings.TrimSpace(imp.Why),
		})
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func extractProductGuidance(scores map[string]any) []ProductGuidanceItem {
	if scores == nil {
		return nil
	}
	raw, ok := scores["product_guidance"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var list []ProductGuidanceItem
	if err := json.Unmarshal(b, &list); err != nil {
		return nil
	}
	return list
}

func extractCareSuggestions(scores map[string]any) []CoachCareSuggestionItem {
	if scores == nil {
		return nil
	}
	raw, ok := scores["care_suggestions"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var list []CoachCareSuggestionItem
	if err := json.Unmarshal(b, &list); err != nil {
		return nil
	}
	out := make([]CoachCareSuggestionItem, 0, len(list))
	for _, c := range list {
		step := strings.TrimSpace(c.Step)
		if step == "" {
			continue
		}
		slot := strings.ToLower(strings.TrimSpace(c.Slot))
		switch slot {
		case "morning", "evening", "today":
		default:
			slot = "today"
		}
		out = append(out, CoachCareSuggestionItem{
			Slot:       slot,
			Step:       step,
			Why:        strings.TrimSpace(c.Why),
			SafetyNote: strings.TrimSpace(c.SafetyNote),
		})
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func extractScoreGauges(scores map[string]any) *SkinCoachScoreGauges {
	if scores == nil {
		return nil
	}
	var out SkinCoachScoreGauges
	set := false
	if x, ok := numFromAny(scores["overall"]); ok {
		out.Overall = x
		set = true
	}
	if x, ok := numFromAny(scores["hydration"]); ok {
		out.Hydration = x
		set = true
	}
	if x, ok := numFromAny(scores["clarity"]); ok {
		out.Clarity = x
		set = true
	}
	if x, ok := numFromAny(scores["barrier"]); ok {
		out.Barrier = x
		set = true
	}
	if !set {
		return nil
	}
	return &out
}

func numFromAny(v any) (*float64, bool) {
	switch n := v.(type) {
	case float64:
		return ptrF64(n), true
	case float32:
		f := float64(n)
		return ptrF64(f), true
	case int:
		return ptrF64(float64(n)), true
	case int64:
		return ptrF64(float64(n)), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return nil, false
		}
		return ptrF64(f), true
	default:
		return nil, false
	}
}

func ptrF64(f float64) *float64 {
	return &f
}

// DecodeStringSlice parses JSON array of strings from RawMessage (nil-safe).
func DecodeStringSlice(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var s []string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return s, nil
}
