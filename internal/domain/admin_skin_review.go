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
type AdminSkinReview struct {
	ID          uuid.UUID       `gorm:"type:uuid;primaryKey" json:"id"`
	AdminUserID uuid.UUID       `gorm:"type:uuid;not null;index" json:"admin_user_id"`
	Title       string          `gorm:"size:200;not null;default:''" json:"title"`
	Notes       string          `gorm:"type:text;not null;default:''" json:"notes"`
	Status      string          `gorm:"size:16;not null;default:draft;index" json:"status"`
	ImagePaths  json.RawMessage `gorm:"type:jsonb;not null;default:'[]'" json:"image_paths"`
	Analysis    json.RawMessage `gorm:"type:jsonb;not null" json:"analysis"`
	Locale      string          `gorm:"size:8;not null;default:vi" json:"locale"`
	ModelUsed   string          `gorm:"size:120;not null;default:''" json:"model_used"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`

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
	return nil
}
