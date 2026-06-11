package dto

import (
	"encoding/json"
	"strings"
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
	// PhotosSkipped is true when the user opted out of face photos during onboarding.
	PhotosSkipped bool `json:"photos_skipped,omitempty"`
	// SkinAnalysis is the vision + coach output from POST /onboarding/analyze-skin.
	// When present, starter-routine generation should ground routine steps and skin_readback on photo observations.
	SkinAnalysis *OnboardingSkinAnalyzeResponse `json:"skin_analysis,omitempty"`
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
	PhotoURLs          []string        `json:"photo_urls,omitempty"`
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
	// StarterRoutinePending is true when the API returned a quick local scaffold
	// and a background job is still generating the personalized AI routine.
	StarterRoutinePending bool `json:"starter_routine_pending,omitempty"`
}

// OnboardingPreviewResponse returns AI starter routine for guests without persisting a profile.
type OnboardingPreviewResponse struct {
	StarterRoutine        StarterRoutineResponse `json:"starter_routine"`
	StarterRoutinePending bool                   `json:"starter_routine_pending,omitempty"`
	PreviewJobID          string                 `json:"preview_job_id,omitempty"`
}

// OnboardingPreviewPollResponse is returned while polling a guest preview job.
type OnboardingPreviewPollResponse struct {
	StarterRoutine        StarterRoutineResponse `json:"starter_routine"`
	StarterRoutinePending bool                   `json:"starter_routine_pending,omitempty"`
}

// DeleteOnboardingResponse is returned by DELETE /profile/onboarding.
type DeleteOnboardingResponse struct {
	DeletedAt string `json:"deleted_at"`
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
	photoURLs := BuildPublicUploadURLs(p.PhotoURLs)
	if len(photoURLs) == 0 {
		photoURLs = photoURLsFromSnapshot(p.OnboardingSnapshot)
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
		PhotoURLs:          photoURLs,
		Version:            p.Version,
		CreatedAt:          p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// BuildPublicUploadURLs converts stored relative paths to `/uploads/...` URLs.
func BuildPublicUploadURLs(raw json.RawMessage) []string {
	rels, _ := DecodeStringSlice(raw)
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		clean := strings.TrimLeft(strings.ReplaceAll(rel, "\\", "/"), "/")
		if clean == "" {
			continue
		}
		out = append(out, "/uploads/"+clean)
	}
	return out
}

func photoURLsFromSnapshot(snap json.RawMessage) []string {
	if len(snap) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(snap, &m); err != nil {
		return nil
	}
	raw, ok := m["photo_urls"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	rels, _ := DecodeStringSlice(b)
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		if strings.HasPrefix(rel, "/uploads/") {
			out = append(out, rel)
			continue
		}
		clean := strings.TrimLeft(strings.ReplaceAll(rel, "\\", "/"), "/")
		out = append(out, "/uploads/"+clean)
	}
	return out
}
