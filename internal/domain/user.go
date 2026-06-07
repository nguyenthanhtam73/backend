package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuthProvider identifies how the user signed up (local, OAuth, etc.).
type AuthProvider string

const (
	AuthProviderLocal  AuthProvider = "local"
	AuthProviderGoogle AuthProvider = "google"
	AuthProviderApple  AuthProvider = "apple"
)

// PlanTier is the subscription bucket for feature gating (beta: free vs premium).
type PlanTier string

const (
	PlanFree    PlanTier = "free"
	PlanPremium PlanTier = "premium"
)

// User is an account that logs skin check-ins and stores skincare data.
type User struct {
	ID           uuid.UUID    `gorm:"type:uuid;primaryKey" json:"id"`
	Email        string       `gorm:"uniqueIndex;size:255;not null" json:"email"`
	Username     string       `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash string       `gorm:"size:255" json:"-"`
	DisplayName  string       `gorm:"size:128" json:"display_name"`
	AvatarURL    string       `gorm:"size:512" json:"avatar_url,omitempty"`
	Provider     AuthProvider `gorm:"size:32;default:local" json:"provider"`
	IsActive     bool         `gorm:"default:true" json:"is_active"`
	PlanTier     PlanTier     `gorm:"size:16;default:free" json:"plan_tier"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (User) TableName() string {
	return "users"
}

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}
