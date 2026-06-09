package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SkillLevel controls verbosity of AI coaching (Beginner / Intermediate / Advanced).
type SkillLevel string

const (
	SkillLevelUnspecified  SkillLevel = "unspecified"
	SkillLevelBeginner     SkillLevel = "beginner"
	SkillLevelIntermediate SkillLevel = "intermediate"
	SkillLevelAdvanced     SkillLevel = "advanced"
)

// SkinProfile stores long-term profile + onboarding snapshot for the AI coach.
type SkinProfile struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"user_id"`

	SkinType           string          `gorm:"size:32" json:"skin_type,omitempty"`
	Concerns           json.RawMessage `gorm:"type:jsonb" json:"concerns,omitempty"`
	Notes              string          `gorm:"type:text" json:"notes,omitempty"`
	SkillLevel         SkillLevel      `gorm:"size:24;default:unspecified" json:"skill_level"`
	OnboardingSnapshot json.RawMessage `gorm:"column:onboarding_snapshot;type:jsonb" json:"onboarding_snapshot,omitempty"`
	// Relative upload paths (userID/onboarding/...) — exposed as `/uploads/...` in API.
	PhotoURLs json.RawMessage `gorm:"column:photo_urls;type:jsonb" json:"photo_urls,omitempty"`
	// Optional region hints for personalization and future weather/routine features (ISO, not Vietnam-only).
	HomeCountryCode string `gorm:"size:2" json:"home_country_code,omitempty"` // ISO 3166-1 alpha-2
	ClimateZone     string `gorm:"size:40" json:"climate_zone,omitempty"`     // e.g. tropical_humid, temperate, arid

	Version int `gorm:"default:1" json:"version"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (SkinProfile) TableName() string {
	return "skin_profiles"
}

func (s *SkinProfile) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
