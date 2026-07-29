package dto

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
)

// AdminSkinAttentionArea is one visible concern region from deep vision analysis.
type AdminSkinAttentionArea struct {
	Region   string `json:"region"`   // e.g. forehead, T-zone, cheeks, chin
	Concern  string `json:"concern"`  // acne, dark_spots, redness, pores, texture, dryness, oiliness, other
	Severity string `json:"severity"` // mild | moderate | pronounced
	Note     string `json:"note"`     // short observation for that region
}

// AdminSkinReviewAnalysis is observations-only AI output (no routine / products / care steps).
type AdminSkinReviewAnalysis struct {
	Overview         string                   `json:"overview"`          // tổng quan tình trạng da
	SkinType         string                   `json:"skin_type"`         // oily | dry | combination | normal | sensitive | unclear
	AttentionAreas   []AdminSkinAttentionArea `json:"attention_areas"`   // vùng chú ý
	OverallSeverity  string                   `json:"overall_severity"`  // mild | moderate | pronounced | clear
	ExtraNotes       string                   `json:"extra_notes"`       // ghi chú quan sát thêm
	NonDiagnostic    string                   `json:"non_diagnostic"`    // mandatory disclaimer
	PhotoQuality     string                   `json:"photo_quality"`     // good | average | poor
	DetailedFindings string                   `json:"detailed_findings"` // longer region-by-region narrative
}

// AdminSkinReviewResponse is the public API shape for create / get / patch.
type AdminSkinReviewResponse struct {
	ID        string                   `json:"id"`
	Title     string                   `json:"title"`
	Notes     string                   `json:"notes"`
	Status    string                   `json:"status"`
	ImageURLs []string                 `json:"image_urls"`
	Analysis  AdminSkinReviewAnalysis  `json:"analysis"`
	Locale    string                   `json:"locale"`
	ModelUsed string                   `json:"model_used"`
	CreatedAt string                   `json:"created_at"`
	UpdatedAt string                   `json:"updated_at"`
}

// PatchAdminSkinReviewRequest updates optional metadata after analysis.
type PatchAdminSkinReviewRequest struct {
	Title  *string `json:"title"`
	Notes  *string `json:"notes"`
	Status *string `json:"status"`
}

// FromDomainAdminSkinReview maps a persisted row + public image URLs to the API DTO.
func FromDomainAdminSkinReview(row *domain.AdminSkinReview, imageURLs []string) AdminSkinReviewResponse {
	if row == nil {
		return AdminSkinReviewResponse{}
	}
	analysis := AdminSkinReviewAnalysis{}
	if len(row.Analysis) > 0 {
		_ = json.Unmarshal(row.Analysis, &analysis)
	}
	if analysis.AttentionAreas == nil {
		analysis.AttentionAreas = []AdminSkinAttentionArea{}
	}
	if imageURLs == nil {
		imageURLs = []string{}
	}
	return AdminSkinReviewResponse{
		ID:        row.ID.String(),
		Title:     row.Title,
		Notes:     row.Notes,
		Status:    row.Status,
		ImageURLs: imageURLs,
		Analysis:  analysis,
		Locale:    row.Locale,
		ModelUsed: row.ModelUsed,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// NormalizeAdminSkinReviewLocale maps locale form/query values to vi|en.
func NormalizeAdminSkinReviewLocale(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "en":
		return "en"
	default:
		return "vi"
	}
}
