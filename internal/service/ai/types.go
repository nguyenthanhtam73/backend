package ai

import "github.com/dadiary/backend/internal/dto"

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
	RoutineHints        []string                `json:"routine_hints"`
	AvoidOrPatch        []string                `json:"avoid_or_patch"`
	SafetyReminders     []string                `json:"safety_reminders"`
	SkinScores          map[string]any          `json:"skin_scores"`
	ConcernAlignment    string                  `json:"concern_alignment"`
	MedicalDisclaimer   string                  `json:"medical_disclaimer"`
	SummaryNotes        string                  `json:"summary_notes"`
	ProductSuggestions  []dto.ProductSuggestion `json:"product_suggestions"`
}
