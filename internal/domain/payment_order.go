package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PaymentOrderStatus tracks the lifecycle of a SePay checkout order.
type PaymentOrderStatus string

const (
	PaymentPending   PaymentOrderStatus = "pending"
	PaymentPaid      PaymentOrderStatus = "paid"
	PaymentFailed    PaymentOrderStatus = "failed"
	PaymentCancelled PaymentOrderStatus = "cancelled"
	PaymentExpired   PaymentOrderStatus = "expired"
)

// PaymentProvider identifies the PSP that owns the checkout session.
type PaymentProvider string

const (
	PaymentProviderSePay PaymentProvider = "sepay"
)

// BillingInterval is monthly or yearly (matches FE pricing catalog).
type BillingInterval string

const (
	BillingMonthly BillingInterval = "monthly"
	BillingYearly  BillingInterval = "yearly"
)

// PaymentOrder is a local record created before redirecting to SePay checkout.
// InvoiceNumber is the merchant-side id sent as order_invoice_number (must be unique).
type PaymentOrder struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	// InvoiceNumber → SePay order_invoice_number (unique, never reused).
	InvoiceNumber string `gorm:"size:64;not null;uniqueIndex" json:"invoice_number"`

	PlanTier        PlanTier           `gorm:"size:16;not null" json:"plan_tier"`
	BillingInterval BillingInterval    `gorm:"size:16;not null" json:"billing_interval"`
	AmountVND       int64              `gorm:"not null" json:"amount_vnd"`
	Currency        string             `gorm:"size:8;not null;default:VND" json:"currency"`
	Status          PaymentOrderStatus `gorm:"size:16;not null;index;default:pending" json:"status"`
	Provider        PaymentProvider    `gorm:"size:16;not null;default:sepay" json:"provider"`

	// Optional SePay identifiers filled from IPN.
	SePayOrderID       string `gorm:"column:se_pay_order_id;size:128" json:"sepay_order_id,omitempty"`
	SePayTransactionID string `gorm:"column:se_pay_transaction_id;size:128" json:"sepay_transaction_id,omitempty"`

	// CustomData is echoed via SePay custom_data (JSON string, not signed).
	CustomData string `gorm:"type:text" json:"custom_data,omitempty"`

	// RawWebhook stores the last IPN body for audit / debugging.
	RawWebhook string `gorm:"type:text" json:"-"`

	PaidAt    *time.Time     `json:"paid_at,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (PaymentOrder) TableName() string {
	return "payment_orders"
}

func (o *PaymentOrder) BeforeCreate(tx *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	if o.Currency == "" {
		o.Currency = "VND"
	}
	if o.Provider == "" {
		o.Provider = PaymentProviderSePay
	}
	if o.Status == "" {
		o.Status = PaymentPending
	}
	return nil
}
