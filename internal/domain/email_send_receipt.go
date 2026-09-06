package domain

import (
	"time"

	"github.com/google/uuid"
)

// EmailSendReceipt records a successful D0/D1 reminder email (≤1 per kind per user).
type EmailSendReceipt struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey" json:"user_id"`
	Kind      string    `gorm:"primaryKey;size:8" json:"kind"` // d0 | d1
	CreatedAt time.Time `json:"created_at"`
}

func (EmailSendReceipt) TableName() string {
	return "email_send_receipts"
}
