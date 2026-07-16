package domain

import (
	"time"

	"github.com/google/uuid"
)

// PushSendReceipt records a successful evening push delivery for one user /
// notification type / Vietnam civil day. Used so job retries after partial
// fan-out (or crash) skip users who already got the nudge.
type PushSendReceipt struct {
	UserID           uuid.UUID `gorm:"type:uuid;primaryKey" json:"user_id"`
	NotificationType string    `gorm:"primaryKey;size:64" json:"notification_type"`
	RunDate          string    `gorm:"primaryKey;size:10" json:"run_date"` // VN 2006-01-02
	CreatedAt        time.Time `json:"created_at"`
}

func (PushSendReceipt) TableName() string {
	return "push_send_receipts"
}
