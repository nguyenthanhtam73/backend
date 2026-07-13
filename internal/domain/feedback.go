package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// FeedbackType categorises user-submitted product feedback (distinct from AI thumbs).
type FeedbackType string

const (
	FeedbackTypeAIFeedback     FeedbackType = "ai_feedback"
	FeedbackTypeBugReport      FeedbackType = "bug_report"
	FeedbackTypeFeatureRequest FeedbackType = "feature_request"
	FeedbackTypeGeneral        FeedbackType = "general"
)

var AllFeedbackTypes = []FeedbackType{
	FeedbackTypeAIFeedback,
	FeedbackTypeBugReport,
	FeedbackTypeFeatureRequest,
	FeedbackTypeGeneral,
}

// IsValidFeedbackType reports whether v is a supported feedback category.
func IsValidFeedbackType(v string) bool {
	for _, t := range AllFeedbackTypes {
		if string(t) == v {
			return true
		}
	}
	return false
}

// FeedbackStatus tracks admin triage state.
type FeedbackStatus string

const (
	FeedbackStatusNew      FeedbackStatus = "new"
	FeedbackStatusRead     FeedbackStatus = "read"
	FeedbackStatusResolved FeedbackStatus = "resolved"
)

var AllFeedbackStatuses = []FeedbackStatus{
	FeedbackStatusNew,
	FeedbackStatusRead,
	FeedbackStatusResolved,
}

// IsValidFeedbackStatus reports whether v is a supported triage status.
func IsValidFeedbackStatus(v string) bool {
	for _, s := range AllFeedbackStatuses {
		if string(s) == v {
			return true
		}
	}
	return false
}

// Feedback is a user-submitted note (bug, feature idea, general opinion).
type Feedback struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	Type      string    `gorm:"size:32;not null;index" json:"type"`
	Comment   string    `gorm:"type:text;not null" json:"comment"`
	Status    string    `gorm:"size:16;not null;default:new;index" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (Feedback) TableName() string {
	return "feedbacks"
}

func (f *Feedback) BeforeCreate(tx *gorm.DB) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	if f.Status == "" {
		f.Status = string(FeedbackStatusNew)
	}
	return nil
}
