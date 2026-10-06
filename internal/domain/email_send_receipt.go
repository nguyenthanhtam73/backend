package domain

import (
	"time"

	"github.com/google/uuid"
)

// EmailSendReceipt records a successful reminder email (≤1 per kind per user).
// Kind is d0, d1, or d3. ResendEmailID is Resend's email id so open/click
// webhooks can join back to the user.
type EmailSendReceipt struct {
	UserID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"user_id"`
	Kind          string    `gorm:"primaryKey;size:8" json:"kind"` // d0 | d1 | d3
	ResendEmailID string    `gorm:"size:64;index" json:"resend_email_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (EmailSendReceipt) TableName() string {
	return "email_send_receipts"
}
