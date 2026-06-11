package dto

// OnboardingSkinObservations holds structured vision cues from the onboarding photo pass.
type OnboardingSkinObservations struct {
	OverallSkinType string `json:"overall_skin_type"`
	TZone           string `json:"t_zone"`
	Cheeks          string `json:"cheeks"`
	PoreSize        string `json:"pore_size"`
	Texture         string `json:"texture"`
	Redness         string `json:"redness"`
	Pigmentation    string `json:"pigmentation"`
	AcneStatus      string `json:"acne_status"`
	OilinessLevel   string `json:"oiliness_level"`
}

// OnboardingSkinAnalyzeResponse is returned by POST /onboarding/analyze-skin (AI vision + structured guesses).
// All guesses are non-diagnostic — users must confirm or edit before persisting profile.
type OnboardingSkinAnalyzeResponse struct {
	SkinTypeGuess    string   `json:"skin_type_guess"`    // dry|oily|combo|normal|sensitive|prefer_not
	UndertoneGuess   string   `json:"undertone_guess"`    // cool|warm|neutral|deep|fair|prefer_not
	Concerns         []string `json:"concerns"`           // stable ids, see onboarding concern vocabulary
	SuggestedGoal    string   `json:"suggested_goal"`     // glow|clear_acne|barrier|anti_aging|unsure
	BarrierSignal    string   `json:"barrier_signal"`     // possibly_compromised|likely_ok|unclear
	Confidence          float64  `json:"confidence"`           // 0–1 model self-rated
	VisualObservations  []string `json:"visual_observations"`  // derived bullets for coach + UI
	CoachingNotes       string   `json:"coaching_notes"`       // coach pass: structured buddy readback + tips
	NonDiagnostic    string   `json:"non_diagnostic"`     // mandatory disclaimer line
	PhotoQuality     struct {
		Sufficient bool     `json:"sufficient"`
		Tips       []string `json:"tips"`
	} `json:"photo_quality"`
	SkinObservations     *OnboardingSkinObservations `json:"skin_observations,omitempty"`
	DetailedObservations string                      `json:"detailed_observations,omitempty"`
	MainConcerns         []string                    `json:"main_concerns,omitempty"` // raw labels from vision (often Vietnamese)
	SkinTone             string                      `json:"skin_tone,omitempty"`
	ModelUsed string `json:"model_used"` // vision model id for transparency
}
