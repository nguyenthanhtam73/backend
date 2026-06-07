package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AnalysisStatus tracks async AI pipeline state for skin photo analysis.
type AnalysisStatus string

const (
	AnalysisStatusPending    AnalysisStatus = "pending"
	AnalysisStatusProcessing AnalysisStatus = "processing"
	AnalysisStatusCompleted  AnalysisStatus = "completed"
	AnalysisStatusFailed     AnalysisStatus = "failed"
)

// SkinAnalysis stores AI coaching output for one SkinCheck.
type SkinAnalysis struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	SkinCheckID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"skin_check_id"`

	Status       AnalysisStatus `gorm:"size:24;default:pending;index" json:"status"`
	// ModelVersion stores e.g. "pipeline=hybrid|vision=gpt-4o(ok)|coach=claude-sonnet-4-6(anthropic)" (~70 chars).
	ModelVersion string `gorm:"size:256" json:"model_version,omitempty"`
	// PromptVersion is the coach pipeline prompt/schema generation (see ai.CoachDailyPromptVersion).
	PromptVersion int `gorm:"default:1;not null" json:"prompt_version,omitempty"`

	SkinScores   json.RawMessage `gorm:"type:jsonb" json:"skin_scores,omitempty"`
	Strengths    json.RawMessage `gorm:"type:jsonb" json:"strengths,omitempty"`
	Improvements json.RawMessage `gorm:"type:jsonb" json:"improvements,omitempty"`
	RoutineHints         json.RawMessage `gorm:"type:jsonb" json:"routine_hints,omitempty"`
	ProductSuggestions   json.RawMessage `gorm:"type:jsonb" json:"product_suggestions,omitempty"`
	AvoidOrPatch         json.RawMessage `gorm:"type:jsonb" json:"avoid_or_patch,omitempty"`
	SummaryNotes string          `gorm:"type:text" json:"summary_notes,omitempty"`
	SafetyFlags  json.RawMessage `gorm:"type:jsonb" json:"safety_flags,omitempty"`
	ErrorMessage string          `gorm:"type:text" json:"error_message,omitempty"`

	AnalyzedAt *time.Time `json:"analyzed_at,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	SkinCheck SkinCheck `gorm:"foreignKey:SkinCheckID" json:"-"`
}

func (SkinAnalysis) TableName() string {
	return "skin_analyses"
}

func (a *SkinAnalysis) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.PromptVersion == 0 {
		a.PromptVersion = 1
	}
	return nil
}
