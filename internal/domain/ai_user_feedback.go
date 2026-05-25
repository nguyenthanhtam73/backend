package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AIFeedbackTarget documents what the user rated (extensible for prompt tuning).
//
// Add new values here whenever a new AI surface ships a "Đúng gu / Chưa hợp"
// vote. Keep the string values stable — they end up in the DB and are read
// back by the prompt loop (BuildPriorFeedbackContext) on every AI call.
type AIFeedbackTarget string

const (
	// AIFeedbackTargetSkinAnalysis — daily check-in coach output on a SkinAnalysis row.
	AIFeedbackTargetSkinAnalysis AIFeedbackTarget = "skin_analysis"
	// AIFeedbackTargetStarterRoutine — onboarding starter routine attached to SkinProfile.
	AIFeedbackTargetStarterRoutine AIFeedbackTarget = "starter_routine"
	// AIFeedbackTargetSuggestedRoutine — ad-hoc AI routine suggestion (POST /routines/suggest);
	// target_id is generated per call (suggestion is not persisted).
	AIFeedbackTargetSuggestedRoutine AIFeedbackTarget = "suggested_routine"
	// AIFeedbackTargetProgressSummary — Progress hero summary (GET /progress, /progress/summary);
	// target_id is generated per page load.
	AIFeedbackTargetProgressSummary AIFeedbackTarget = "progress_summary"
	// AIFeedbackTargetDailyCheckIn — alias for skin_analysis votes when the UI
	// fires before SkinAnalysis.id is known. Treat the same as skin_analysis
	// in the prompt loop.
	AIFeedbackTargetDailyCheckIn AIFeedbackTarget = "daily_check_in"
)

// AllAIFeedbackTargets is the canonical allow-list used by request validators.
var AllAIFeedbackTargets = []AIFeedbackTarget{
	AIFeedbackTargetSkinAnalysis,
	AIFeedbackTargetStarterRoutine,
	AIFeedbackTargetSuggestedRoutine,
	AIFeedbackTargetProgressSummary,
	AIFeedbackTargetDailyCheckIn,
}

// IsValidAIFeedbackTarget reports whether v is one of the supported targets.
func IsValidAIFeedbackTarget(v string) bool {
	for _, t := range AllAIFeedbackTargets {
		if string(t) == v {
			return true
		}
	}
	return false
}

// AIFeedbackRating is thumbs-style signal for the feedback loop.
type AIFeedbackRating string

const (
	AIFeedbackHelpful    AIFeedbackRating = "helpful"
	AIFeedbackNotHelpful AIFeedbackRating = "not_helpful"
)

// AIUserFeedback stores explicit user signal on AI output quality (for future prompt / eval loops).
type AIUserFeedback struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID     uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	TargetType string    `gorm:"size:32;not null;index" json:"target_type"` // AIFeedbackTarget values
	TargetID   uuid.UUID `gorm:"type:uuid;not null;index" json:"target_id"` // e.g. skin_analyses.id
	Rating     string    `gorm:"size:24;not null" json:"rating"`            // helpful | not_helpful
	Comment    string    `gorm:"type:text" json:"comment,omitempty"`
	CreatedAt  time.Time `json:"created_at"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (AIUserFeedback) TableName() string {
	return "ai_user_feedback"
}

func (f *AIUserFeedback) BeforeCreate(tx *gorm.DB) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return nil
}
