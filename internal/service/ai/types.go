package ai

import "github.com/dadiary/backend/internal/dto"

// CoachCareSuggestion is one in-app care step (richer than public Admin Skin Review soothing_tips).
// Public /share/skin-review must NOT surface this field — check-in coach only.
type CoachCareSuggestion struct {
	// Slot: morning | evening | today (UI groups by slot).
	Slot string `json:"slot"`
	// Step: everyday step name (e.g. "Rửa mặt dịu", "Dưỡng ẩm", "Chống nắng").
	Step string `json:"step"`
	// Why: one plain sentence tying the step to today's skin read.
	Why string `json:"why"`
	// SafetyNote: optional caution (avoid picking, ease strong actives when inflamed…).
	SafetyNote string `json:"safety_note,omitempty"`
}

// CoachStructuredOutput is the JSON contract for DaDiary coach (Claude primary / GPT fallback).
// Field names are stable for DB mapping in the analysis worker.
type CoachStructuredOutput struct {
	Score             float64  `json:"score"`
	Strengths         []string `json:"strengths"`
	SituationAnalysis string   `json:"situation_analysis"`
	Improvements      []struct {
		Tip string `json:"tip"`
		Why string `json:"why"`
	} `json:"improvements"`
	// CareSuggestions is the in-app detailed care checklist (AM/PM/today + why + safety).
	// Persisted inside skin_scores JSON under key "care_suggestions" (no DB migration).
	CareSuggestions     []CoachCareSuggestion   `json:"care_suggestions"`
	RoutineHints        []string                `json:"routine_hints"`
	AvoidOrPatch        []string                `json:"avoid_or_patch"`
	SafetyReminders     []string                `json:"safety_reminders"`
	SkinScores          map[string]any          `json:"skin_scores"`
	ConcernAlignment    string                  `json:"concern_alignment"`
	MedicalDisclaimer   string                  `json:"medical_disclaimer"`
	SummaryNotes        string                     `json:"summary_notes"`
	ProductSuggestions  []dto.ProductSuggestion    `json:"product_suggestions"`
	ProductGuidance     []dto.ProductGuidanceItem  `json:"product_guidance,omitempty"`
	CarePhase           string                     `json:"care_phase,omitempty"` // calm_first | can_add_active
}
