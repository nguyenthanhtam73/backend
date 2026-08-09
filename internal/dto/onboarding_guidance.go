package dto

// OnboardingProductGuidanceRequest asks for product guidance without photos.
// Every field mirrors an onboarding answer the user already gave on step 1.
type OnboardingProductGuidanceRequest struct {
	Locale   string   `json:"locale"`
	SkinType string   `json:"skin_type"` // dry|oily|combo|normal|sensitive|prefer_not
	Goal     string   `json:"goal"`      // glow|clear_acne|barrier|anti_aging|unsure
	Concerns []string `json:"concerns"`  // stable concern ids picked on step 1
}

// OnboardingProductGuidanceResponse mirrors the commerce half of analyze-skin.
type OnboardingProductGuidanceResponse struct {
	Phase              string                `json:"phase"`
	ProductGuidance    []ProductGuidanceItem `json:"product_guidance"`
	ProductSuggestions []ProductSuggestion   `json:"product_suggestions"`
}
