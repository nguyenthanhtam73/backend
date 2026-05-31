package dto

import (
	"encoding/json"
	"time"

	"github.com/dadiary/backend/internal/domain"
)

// OnboardingCompleteRequest is sent when the user finishes the onboarding wizard.
type OnboardingCompleteRequest struct {
	SkinType        string   `json:"skin_type"`
	Undertone       string   `json:"undertone"`
	Contexts        []string `json:"contexts"`
	Budget          string   `json:"budget"`
	Goal            string   `json:"goal"`
	SkillLevel      string   `json:"skill_level"` // beginner | intermediate | advanced
	BodyConcerns    []string `json:"body_concerns,omitempty"`
	CurrentRoutine  string   `json:"current_routine,omitempty"`
	HomeCountryCode string   `json:"home_country_code,omitempty"`
	// Locale is the app UI language when finishing onboarding ("vi" | "en"). Drives starter-routine copy language.
	Locale string `json:"locale,omitempty"`
}

// SkinProfileResponse is the public API shape for GET /profile/skin.
type SkinProfileResponse struct {
	ID                 string          `json:"id"`
	UserID             string          `json:"user_id"`
	SkinType           string          `json:"skin_type,omitempty"`
	SkillLevel         string          `json:"skill_level"`
	Concerns           []string        `json:"concerns,omitempty"`
	Notes              string          `json:"notes,omitempty"`
	HomeCountryCode    string          `json:"home_country_code,omitempty"`
	ClimateZone        string          `json:"climate_zone,omitempty"`
	OnboardingSnapshot json.RawMessage `json:"onboarding_snapshot,omitempty"`
	Version            int             `json:"version"`
	CreatedAt          string          `json:"created_at"`
	UpdatedAt          string          `json:"updated_at"`
}

// StarterRoutineResponse is the AI-generated scaffold returned after onboarding.
type StarterRoutineResponse struct {
	Morning     []string `json:"morning"`
	Evening     []string `json:"evening"`
	WeekNotes   string   `json:"week_notes"`
	SafetyNotes string   `json:"safety_notes"`
	// Encouragement, skin read-back, rationale, and closing supportive line (Vietnamese or English per user context).
	Encouragement   string `json:"encouragement"`
	SkinReadback    string `json:"skin_readback"`
	Rationale       string `json:"rationale"`
	ClosingReminder string `json:"closing_reminder"`
	ProductSuggestions []ProductSuggestion `json:"product_suggestions,omitempty"`
}

// OnboardingCompleteResponse returns saved profile + AI starter routine scaffold.
type OnboardingCompleteResponse struct {
	Profile        SkinProfileResponse    `json:"profile"`
	StarterRoutine StarterRoutineResponse `json:"starter_routine"`
}

// PutSkinProfileRequest allows partial manual edits (no AI).
type PutSkinProfileRequest struct {
	SkinType           *string         `json:"skin_type,omitempty"`
	SkillLevel         *string         `json:"skill_level,omitempty"`
	Concerns           []string        `json:"concerns,omitempty"`
	Notes              *string         `json:"notes,omitempty"`
	HomeCountryCode    *string         `json:"home_country_code,omitempty"`
	ClimateZone        *string         `json:"climate_zone,omitempty"`
	OnboardingSnapshot json.RawMessage `json:"onboarding_snapshot,omitempty"`
}

// SkinProfileFromDomain maps DB row to API DTO.
func SkinProfileFromDomain(p *domain.SkinProfile) SkinProfileResponse {
	if p == nil {
		return SkinProfileResponse{}
	}
	var concerns []string
	if len(p.Concerns) > 0 {
		_ = json.Unmarshal(p.Concerns, &concerns)
	}
	return SkinProfileResponse{
		ID:                 p.ID.String(),
		UserID:             p.UserID.String(),
		SkinType:           p.SkinType,
		SkillLevel:         string(p.SkillLevel),
		Concerns:           concerns,
		Notes:              p.Notes,
		HomeCountryCode:    p.HomeCountryCode,
		ClimateZone:        p.ClimateZone,
		OnboardingSnapshot: append(json.RawMessage(nil), p.OnboardingSnapshot...),
		Version:            p.Version,
		CreatedAt:          p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
