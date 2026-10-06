package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PushClickEvent records a client-reported notification click.
// (user_id, idempotency_key) is unique so a double tap does not double-count.
type PushClickEvent struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID           uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_push_click_user_idem,priority:1" json:"user_id"`
	NotificationKind string    `gorm:"size:64;not null;default:''" json:"notification_kind,omitempty"`
	Tag              string    `gorm:"size:128;not null;default:''" json:"tag,omitempty"`
	IdempotencyKey   string    `gorm:"size:128;not null;uniqueIndex:idx_push_click_user_idem,priority:2" json:"idempotency_key"`
	ClickedAt        time.Time `gorm:"not null;index" json:"clicked_at"`
	CreatedAt        time.Time `json:"created_at"`
}

func (PushClickEvent) TableName() string {
	return "push_click_events"
}

func (e *PushClickEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}
