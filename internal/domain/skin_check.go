package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CheckVisibility controls optional sharing of a skin check-in.
type CheckVisibility string

const (
	CheckVisibilityPrivate CheckVisibility = "private"
	CheckVisibilityPublic  CheckVisibility = "public"
)

// SkinCheck is a daily diary entry: face photo(s) + self-reported conditions + notes.
type SkinCheck struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	Title           string          `gorm:"size:200" json:"title,omitempty"`
	UserNote        string          `gorm:"type:text" json:"user_note,omitempty"`
	ImageURLs       json.RawMessage `gorm:"type:jsonb;not null" json:"image_urls"`
	Conditions      json.RawMessage `gorm:"type:jsonb" json:"conditions,omitempty"`      // []string SkinCondition
	Symptoms        json.RawMessage `gorm:"type:jsonb" json:"symptoms,omitempty"`        // []string SkinSymptom
	ClimateContext  json.RawMessage `gorm:"type:jsonb" json:"climate_context,omitempty"` // ClimateSnapshot
	EnvironmentNote string          `gorm:"size:128" json:"environment_note,omitempty"`  // sleep, stress, manual weather note
	Visibility      CheckVisibility `gorm:"size:16;default:private" json:"visibility"`
	CheckDate       time.Time       `gorm:"type:date;not null;index" json:"check_date"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	User     User          `gorm:"foreignKey:UserID" json:"-"`
	Analysis *SkinAnalysis `gorm:"foreignKey:SkinCheckID" json:"analysis,omitempty"`
}

func (SkinCheck) TableName() string {
	return "skin_checks"
}

func (s *SkinCheck) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
