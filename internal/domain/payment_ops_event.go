package domain

import (
	"time"

	"github.com/google/uuid"
)

// Payment ops event kinds persisted for admin metrics / alerting.
const (
	OpsKindWebhookError   = "webhook_error"
	OpsKindPaymentFail    = "payment_fail"
	OpsKindPaymentSuccess = "payment_success"
)

// PaymentOpsEvent is a lightweight audit row for payment monitoring.
type PaymentOpsEvent struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Kind          string    `gorm:"size:32;not null;index:idx_payment_ops_kind_created,priority:1" json:"kind"`
	Reason        string    `gorm:"size:64;not null;default:''" json:"reason,omitempty"`
	InvoiceNumber string    `gorm:"size:64;not null;default:''" json:"invoice_number,omitempty"`
	CreatedAt     time.Time `gorm:"index:idx_payment_ops_kind_created,priority:2" json:"created_at"`
}

func (PaymentOpsEvent) TableName() string {
	return "payment_ops_events"
}
