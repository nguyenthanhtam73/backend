package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AdminSkinReviewStatus is the optional publish state for a saved admin review.
type AdminSkinReviewStatus string

const (
	AdminSkinReviewStatusDraft     AdminSkinReviewStatus = "draft"
	AdminSkinReviewStatusPublished AdminSkinReviewStatus = "published"
)

// IsValidAdminSkinReviewStatus reports whether v is draft or published.
func IsValidAdminSkinReviewStatus(v string) bool {
	switch AdminSkinReviewStatus(v) {
	case AdminSkinReviewStatusDraft, AdminSkinReviewStatusPublished:
		return true
	default:
		return false
	}
}

// AdminSkinReview stores an admin-only skin observation session (no routine).
// Photos + AI analysis JSON are persisted so admins can reopen reviews later.
// When IsPublic, PublicSlug unlocks the unauthenticated share page.
type AdminSkinReview struct {
	ID          uuid.UUID       `gorm:"type:uuid;primaryKey" json:"id"`
	AdminUserID uuid.UUID       `gorm:"type:uuid;not null;index" json:"admin_user_id"`
	Title       string          `gorm:"size:200;not null;default:''" json:"title"`
	Notes       string          `gorm:"type:text;not null;default:''" json:"notes"`
	// UserQuestion is the FB/group question the admin is answering (public when set).
	UserQuestion string `gorm:"type:text;not null;default:''" json:"user_question"`
	// Answer is the admin/AI reply shown on share + PNG export (public when set).
	Answer string `gorm:"type:text;not null;default:''" json:"answer"`
	Status string `gorm:"size:16;not null;default:draft;index" json:"status"`
	// SkinContext holds the operator's touch / pain / duration answers. A photo cannot
	// separate milia from closed comedones from skin tags; these answers can, so they are
	// persisted and reused on reanalyze.
	SkinContext string          `gorm:"type:text;not null;default:''" json:"skin_context"`
	ImagePaths  json.RawMessage `gorm:"type:jsonb;not null;default:'[]'" json:"image_paths"`
	// PublicImagePaths are privacy-blurred copies served on the public share page.
	PublicImagePaths json.RawMessage `gorm:"type:jsonb;not null;default:'[]'" json:"public_image_paths"`
	Analysis         json.RawMessage `gorm:"type:jsonb;not null" json:"analysis"`
	// AnalysisOriginal keeps the model's first answer once an operator corrects Analysis.
	// The (original, corrected) pair is the labeled dataset for measuring accuracy —
	// operators already do this labeling work while reviewing, so it must not be thrown away.
	AnalysisOriginal    json.RawMessage `gorm:"type:jsonb" json:"analysis_original,omitempty"`
	AnalysisCorrectedAt *time.Time      `json:"analysis_corrected_at,omitempty"`
	Locale              string          `gorm:"size:8;not null;default:vi" json:"locale"`
	ModelUsed           string          `gorm:"size:120;not null;default:''" json:"model_used"`

	IsPublic    bool       `gorm:"not null;default:false;index" json:"is_public"`
	// PublicSlug uniqueness for non-empty values is enforced by migration 011 partial index.
	PublicSlug  string     `gorm:"size:32;index" json:"public_slug"`
	PublishedAt *time.Time `json:"published_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	AdminUser User `gorm:"foreignKey:AdminUserID" json:"-"`
}

func (AdminSkinReview) TableName() string {
	return "admin_skin_reviews"
}

func (r *AdminSkinReview) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	if r.Status == "" {
		r.Status = string(AdminSkinReviewStatusDraft)
	}
	if r.Locale == "" {
		r.Locale = "vi"
	}
	if len(r.ImagePaths) == 0 {
		r.ImagePaths = json.RawMessage("[]")
	}
	if len(r.PublicImagePaths) == 0 {
		r.PublicImagePaths = json.RawMessage("[]")
	}
	return nil
}
