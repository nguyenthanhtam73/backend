package dto

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
)

// AdminSkinAttentionArea is one visible concern region from deep vision analysis.
type AdminSkinAttentionArea struct {
	Region   string `json:"region"`
	Concern  string `json:"concern"`
	Severity string `json:"severity"`
	Note     string `json:"note"`
}

// AdminSkinReviewAnalysis is observations-only AI output (no routine / products / care steps).
type AdminSkinReviewAnalysis struct {
	Overview               string                   `json:"overview"`
	SkinType               string                   `json:"skin_type"`
	SkinTypeSeverity       string                   `json:"skin_type_severity"`
	SkinTypeNote           string                   `json:"skin_type_note,omitempty"`
	AttentionAreas         []AdminSkinAttentionArea `json:"attention_areas"`
	AdditionalObservations string                   `json:"additional_observations"`
	PhotoNotes             string                   `json:"photo_notes"`
	NonDiagnostic          string                   `json:"non_diagnostic"`

	// Legacy fields kept for unmarshaling older saved reviews.
	OverallSeverity  string `json:"overall_severity,omitempty"`
	ExtraNotes       string `json:"extra_notes,omitempty"`
	DetailedFindings string `json:"detailed_findings,omitempty"`
	PhotoQuality     string `json:"photo_quality,omitempty"`
}

// AdminSkinReviewResponse is the admin API shape for create / get / patch / publish.
type AdminSkinReviewResponse struct {
	ID          string                  `json:"id"`
	Title       string                  `json:"title"`
	Notes       string                  `json:"notes"`
	Status      string                  `json:"status"`
	ImageURLs   []string                `json:"image_urls"`
	Analysis    AdminSkinReviewAnalysis `json:"analysis"`
	Locale      string                  `json:"locale"`
	ModelUsed   string                  `json:"model_used"`
	IsPublic    bool                    `json:"is_public"`
	PublicSlug  string                  `json:"public_slug,omitempty"`
	PublishedAt string                  `json:"published_at,omitempty"`
	SharePath   string                  `json:"share_path,omitempty"` // e.g. /share/skin-review/{slug}
	CreatedAt   string                  `json:"created_at"`
	UpdatedAt   string                  `json:"updated_at"`
}

// PublicSkinReviewResponse is the unauthenticated share payload.
// Omits admin internal notes and original (unblurred) image paths.
type PublicSkinReviewResponse struct {
	Slug          string                  `json:"slug"`
	Title         string                  `json:"title"`
	Analysis      AdminSkinReviewAnalysis `json:"analysis"`
	ImageURLs     []string                `json:"image_urls"` // privacy-blurred only
	ImagesBlurred bool                    `json:"images_blurred"`
	Locale        string                  `json:"locale"`
	PublishedAt   string                  `json:"published_at,omitempty"`
	SharePath     string                  `json:"share_path"`
}

// PatchAdminSkinReviewRequest updates optional metadata after analysis.
type PatchAdminSkinReviewRequest struct {
	Title  *string `json:"title"`
	Notes  *string `json:"notes"`
	Status *string `json:"status"`
}

// AdminSkinReviewListItem is a compact row for the admin list table.
type AdminSkinReviewListItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	IsPublic    bool   `json:"is_public"`
	PublicSlug  string `json:"public_slug,omitempty"`
	SharePath   string `json:"share_path,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Locale      string `json:"locale"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// AdminSkinReviewListResponse is paginated admin list payload.
type AdminSkinReviewListResponse struct {
	Items    []AdminSkinReviewListItem `json:"items"`
	Total    int64                     `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
}

// FromDomainAdminSkinReviewListItem maps a row to the list DTO.
func FromDomainAdminSkinReviewListItem(row domain.AdminSkinReview) AdminSkinReviewListItem {
	out := AdminSkinReviewListItem{
		ID:         row.ID.String(),
		Title:      row.Title,
		Status:     row.Status,
		IsPublic:   row.IsPublic,
		PublicSlug: row.PublicSlug,
		Locale:     row.Locale,
		CreatedAt:  row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.PublishedAt != nil {
		out.PublishedAt = row.PublishedAt.UTC().Format(time.RFC3339)
	}
	if row.IsPublic && row.PublicSlug != "" {
		out.SharePath = "/share/skin-review/" + row.PublicSlug
	}
	return out
}

// FromDomainAdminSkinReview maps a persisted row + public image URLs to the admin API DTO.
func FromDomainAdminSkinReview(row *domain.AdminSkinReview, imageURLs []string) AdminSkinReviewResponse {
	if row == nil {
		return AdminSkinReviewResponse{}
	}
	analysis := AdminSkinReviewAnalysis{}
	if len(row.Analysis) > 0 {
		_ = json.Unmarshal(row.Analysis, &analysis)
	}
	NormalizeAdminSkinReviewAnalysis(&analysis, row.Locale)
	if imageURLs == nil {
		imageURLs = []string{}
	}
	out := AdminSkinReviewResponse{
		ID:         row.ID.String(),
		Title:      row.Title,
		Notes:      row.Notes,
		Status:     row.Status,
		ImageURLs:  imageURLs,
		Analysis:   analysis,
		Locale:     row.Locale,
		ModelUsed:  row.ModelUsed,
		IsPublic:   row.IsPublic,
		PublicSlug: row.PublicSlug,
		CreatedAt:  row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.PublishedAt != nil {
		out.PublishedAt = row.PublishedAt.UTC().Format(time.RFC3339)
	}
	if row.IsPublic && row.PublicSlug != "" {
		out.SharePath = "/share/skin-review/" + row.PublicSlug
	}
	return out
}

// FromDomainPublicSkinReview maps a public row to the share DTO (no admin notes).
func FromDomainPublicSkinReview(row *domain.AdminSkinReview, blurredURLs []string) PublicSkinReviewResponse {
	if row == nil {
		return PublicSkinReviewResponse{}
	}
	analysis := AdminSkinReviewAnalysis{}
	if len(row.Analysis) > 0 {
		_ = json.Unmarshal(row.Analysis, &analysis)
	}
	NormalizeAdminSkinReviewAnalysis(&analysis, row.Locale)
	if blurredURLs == nil {
		blurredURLs = []string{}
	}
	out := PublicSkinReviewResponse{
		Slug:          row.PublicSlug,
		Title:         strings.TrimSpace(row.Title),
		Analysis:      analysis,
		ImageURLs:     blurredURLs,
		ImagesBlurred: true,
		Locale:        row.Locale,
		SharePath:     "/share/skin-review/" + row.PublicSlug,
	}
	if row.PublishedAt != nil {
		out.PublishedAt = row.PublishedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// NormalizeAdminSkinReviewAnalysis fills new fields from legacy keys when needed.
func NormalizeAdminSkinReviewAnalysis(a *AdminSkinReviewAnalysis, locale string) {
	if a == nil {
		return
	}
	if a.AttentionAreas == nil {
		a.AttentionAreas = []AdminSkinAttentionArea{}
	}
	if strings.TrimSpace(a.SkinTypeSeverity) == "" && strings.TrimSpace(a.OverallSeverity) != "" {
		a.SkinTypeSeverity = a.OverallSeverity
	}
	a.SkinTypeNote = strings.TrimSpace(a.SkinTypeNote)
	if strings.TrimSpace(a.AdditionalObservations) == "" {
		parts := make([]string, 0, 2)
		if s := strings.TrimSpace(a.DetailedFindings); s != "" {
			parts = append(parts, s)
		}
		if s := strings.TrimSpace(a.ExtraNotes); s != "" {
			parts = append(parts, s)
		}
		a.AdditionalObservations = strings.Join(parts, "\n\n")
	}
	if strings.TrimSpace(a.PhotoNotes) == "" {
		a.PhotoNotes = defaultPhotoNotesFromQuality(a.PhotoQuality, locale)
	}
	a.OverallSeverity = ""
	a.ExtraNotes = ""
	a.DetailedFindings = ""
	a.PhotoQuality = ""
}

func defaultPhotoNotesFromQuality(quality, locale string) string {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "good":
		if locale == "en" {
			return "Photos are clear enough for review"
		}
		return "Ảnh đủ rõ để nhận xét"
	case "poor":
		if locale == "en" {
			return "Photo quality is limited (lighting, angle, or blur) — review may be incomplete."
		}
		return "Ảnh chưa rõ (ánh sáng, góc, hoặc bị mờ) — nhận xét có thể chưa đủ."
	case "average":
		if locale == "en" {
			return "Photo quality is average — some cues may be harder to judge."
		}
		return "Chất lượng ảnh trung bình — một số dấu hiệu có thể khó đánh giá."
	default:
		if locale == "en" {
			return "Photos are clear enough for review"
		}
		return "Ảnh đủ rõ để nhận xét"
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
