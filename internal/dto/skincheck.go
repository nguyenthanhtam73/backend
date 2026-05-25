package dto

import (
	"encoding/json"
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// CreateSkinCheckResponse is returned after POST /api/v1/skin-checks succeeds.
// When AI runs synchronously, analysis.coach contains structured coach feedback (or error_message on pipeline failure).
type CreateSkinCheckResponse struct {
	Check     SkinCheckSummary    `json:"check"`
	Analysis  SkinAnalysisSummary `json:"analysis"`
	ImageURLs []string            `json:"image_urls"`
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

// SkinCoachDetail is structured AI feedback for the daily check-in UI (Claude primary pipeline).
type SkinCoachDetail struct {
	SummaryNotes       string                 `json:"summary_notes,omitempty"`
	Strengths          []string               `json:"strengths,omitempty"`
	SituationSummary   string                 `json:"situation_summary,omitempty"`
	ConcernAlignment   string                 `json:"concern_alignment,omitempty"`
	SkinScoreGauges    *SkinCoachScoreGauges  `json:"skin_score_gauges,omitempty"`
	Improvements       []CoachImprovementItem `json:"improvements,omitempty"`
	RoutineHints       []string               `json:"routine_hints,omitempty"`
	AvoidOrPatch       []string               `json:"avoid_or_patch,omitempty"`
	SafetyReminders    []string               `json:"safety_reminders,omitempty"`
	MedicalDisclaimer  string                 `json:"medical_disclaimer,omitempty"`
	ErrorMessage       string                 `json:"error_message,omitempty"`
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
			g := extractScoreGauges(scores)
			if g != nil {
				d.SkinScoreGauges = g
			}
		}
	}
	return d
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
