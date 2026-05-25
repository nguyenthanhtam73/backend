package dto

// OnboardingSkinAnalyzeResponse is returned by POST /onboarding/analyze-skin (AI vision + structured guesses).
// All guesses are non-diagnostic — users must confirm or edit before persisting profile.
type OnboardingSkinAnalyzeResponse struct {
	SkinTypeGuess    string   `json:"skin_type_guess"`    // dry|oily|combo|normal|sensitive|prefer_not
	UndertoneGuess   string   `json:"undertone_guess"`    // cool|warm|neutral|deep|fair|prefer_not
	Concerns         []string `json:"concerns"`           // stable ids, see onboarding concern vocabulary
	SuggestedGoal    string   `json:"suggested_goal"`     // glow|clear_acne|barrier|anti_aging|unsure
	BarrierSignal    string   `json:"barrier_signal"`     // possibly_compromised|likely_ok|unclear
	Confidence       float64  `json:"confidence"`         // 0–1 model self-rated
	CoachingNotes    string   `json:"coaching_notes"`     // short supportive summary
	NonDiagnostic    string   `json:"non_diagnostic"`     // mandatory disclaimer line
	PhotoQuality     struct {
		Sufficient bool     `json:"sufficient"`
		Tips       []string `json:"tips"`
	} `json:"photo_quality"`
	ModelUsed string `json:"model_used"` // vision model id for transparency
}
