package domain

import (
	"time"

	"github.com/google/uuid"
)

// EmailSendReceipt records one successful outbound email per (user, kind).
// Kind is d0, d1, d3, or a one-shot marker such as ai_outage_apology_20261106.
// ResendEmailID is Resend's email id so open/click webhooks can join back to the user.
type EmailSendReceipt struct {
	UserID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"user_id"`
	Kind          string    `gorm:"primaryKey;size:64" json:"kind"`
	ResendEmailID string    `gorm:"size:64;index" json:"resend_email_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (EmailSendReceipt) TableName() string {
	return "email_send_receipts"
}
