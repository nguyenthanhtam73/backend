package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Email engagement event types stored from the Resend webhook.
const (
	EmailEventOpened  = "opened"
	EmailEventClicked = "clicked"
)

// EmailEngagementEvent is one Resend open or click. ResendEventID is the
// Svix message id; duplicate deliveries insert nothing.
type EmailEngagementEvent struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	ResendEventID string     `gorm:"size:128;not null;uniqueIndex" json:"resend_event_id"`
	ResendEmailID string     `gorm:"size:64;not null;default:'';index" json:"resend_email_id"`
	UserID        *uuid.UUID `gorm:"type:uuid;index" json:"user_id,omitempty"`
	Kind          string     `gorm:"size:8;not null;default:''" json:"kind,omitempty"` // d0 | d1 | d3
	EventType     string     `gorm:"size:16;not null" json:"event_type"`               // opened | clicked
	LinkURL       string     `gorm:"size:2048;not null;default:''" json:"link_url,omitempty"`
	OccurredAt    time.Time  `gorm:"not null" json:"occurred_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (EmailEngagementEvent) TableName() string {
	return "email_engagement_events"
}

func (e *EmailEngagementEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}
