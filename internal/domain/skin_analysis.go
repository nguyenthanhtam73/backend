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

// AIAnalysisOutageSince is the start of the November 2026 credit-balance
// window (a little before the first provider 400/429). Photo check-ins and
// onboarding profiles from this instant are the replay population.
var AIAnalysisOutageSince = time.Date(2026, 11, 6, 13, 0, 0, 0, time.UTC)

// DeferredAnalysisMessage is the user-facing skin_analyses.error_message stored
// when the photo was saved but the coach could not finish because the provider
// was out of credit or the analysis pipeline is down. It is Vietnamese because
// error_message is shown to the user. Clients also receive analysis_pending.
const DeferredAnalysisMessage = "Mình đang phân tích lại. Kết quả sẽ có sau — bạn chưa cần gửi lại."

// SkinAnalysis stores AI coaching output for one SkinCheck.
type SkinAnalysis struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	SkinCheckID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"skin_check_id"`

	Status AnalysisStatus `gorm:"size:24;default:pending;index" json:"status"`
	// ModelVersion stores e.g. "pipeline=hybrid|vision=gpt-4o(ok)|coach=claude-sonnet-4-6(anthropic)" (~70 chars).
	// Android voice appends "+android" after that string. Split on "|" still yields
	// pipeline, vision, and coach; the suffix is not its own segment.
	ModelVersion string `gorm:"size:256" json:"model_version,omitempty"`
	// PromptVersion is the coach pipeline prompt/schema generation (see ai.CoachDailyPromptVersion).
	PromptVersion int `gorm:"default:1;not null" json:"prompt_version,omitempty"`

	SkinScores         json.RawMessage `gorm:"type:jsonb" json:"skin_scores,omitempty"`
	Strengths          json.RawMessage `gorm:"type:jsonb" json:"strengths,omitempty"`
	Improvements       json.RawMessage `gorm:"type:jsonb" json:"improvements,omitempty"`
	RoutineHints       json.RawMessage `gorm:"type:jsonb" json:"routine_hints,omitempty"`
	ProductSuggestions json.RawMessage `gorm:"type:jsonb" json:"product_suggestions,omitempty"`
	AvoidOrPatch       json.RawMessage `gorm:"type:jsonb" json:"avoid_or_patch,omitempty"`
	SummaryNotes       string          `gorm:"type:text" json:"summary_notes,omitempty"`
	SafetyFlags        json.RawMessage `gorm:"type:jsonb" json:"safety_flags,omitempty"`
	ErrorMessage       string          `gorm:"type:text" json:"error_message,omitempty"`

	AnalyzedAt *time.Time `json:"analyzed_at,omitempty"`
	// LastReanalyzedAt is set when the owner claims a reanalyze job. Used to
	// cap successful re-runs at one per check per UTC day (failed retries are
	// allowed so a dead pipeline does not lock the photo).
	LastReanalyzedAt *time.Time `json:"last_reanalyzed_at,omitempty"`

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
