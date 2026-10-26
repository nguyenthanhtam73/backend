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
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	// Composite index (user_id, check_date) speeds HasCheckedInToday / daily reminder.
	UserID uuid.UUID `gorm:"type:uuid;not null;index;index:idx_skin_checks_user_check_date,priority:1" json:"user_id"`

	Title           string          `gorm:"size:200" json:"title,omitempty"`
	UserNote        string          `gorm:"type:text" json:"user_note,omitempty"`
	ImageURLs       json.RawMessage `gorm:"type:jsonb;not null" json:"image_urls"`
	Conditions      json.RawMessage `gorm:"type:jsonb" json:"conditions,omitempty"`      // []string SkinCondition
	Symptoms        json.RawMessage `gorm:"type:jsonb" json:"symptoms,omitempty"`        // []string SkinSymptom
	ClimateContext  json.RawMessage `gorm:"type:jsonb" json:"climate_context,omitempty"` // ClimateSnapshot
	EnvironmentNote string          `gorm:"size:128" json:"environment_note,omitempty"`  // sleep, stress, manual weather note
	Visibility      CheckVisibility `gorm:"size:16;default:private" json:"visibility"`
	CheckDate       time.Time       `gorm:"type:date;not null;index;index:idx_skin_checks_user_check_date,priority:2" json:"check_date"`
	// ClientKind selects the coach voice for this check. "android" is the polite
	// mình/bạn voice required for the Play app. Anything else, including empty,
	// is the existing web voice. Existing rows default to web (AutoMigrate and
	// migrations/027_skin_check_client_kind.up.sql). Not part of the public API.
	ClientKind string `gorm:"column:client_kind;size:16;not null;default:'web'" json:"-"`
	// PhotoContext stores optional close-up metadata and touch answers for this
	// check. Shape: {"images":[{"index":0,"kind":"closeup","zone":"left_cheek"}],
	// "skin_context":{"firmness":"firm",...}}. Null on older rows (no backfill).
	// Included in GET /me/export. Account deletion removes it with this row
	// (migrations/028_skin_check_photo_context.up.sql and AutoMigrate).
	PhotoContext json.RawMessage `gorm:"column:photo_context;type:jsonb" json:"-"`

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
