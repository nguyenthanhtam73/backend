package dto

// CreateSePayCheckoutRequest is POST /api/v1/payment/sepay/checkout.
type CreateSePayCheckoutRequest struct {
	PlanTier        string `json:"plan_tier"`        // premium | premium_plus
	BillingInterval string `json:"billing_interval"` // monthly | yearly
	// Locale is the UI locale (vi | en) — used for SePay success/error/cancel URLs.
	Locale string `json:"locale,omitempty"`
	// PaymentMethod optional: CARD | BANK_TRANSFER | NAPAS_BANK_TRANSFER.
	// Empty lets the customer choose on SePay's page.
	PaymentMethod string `json:"payment_method,omitempty"`
}

// CreateSePayCheckoutResponse returns everything the FE needs to auto-POST a form.
type CreateSePayCheckoutResponse struct {
	OrderID       string            `json:"order_id"`
	InvoiceNumber string            `json:"invoice_number"`
	PlanTier      string            `json:"plan_tier"`
	BillingInterval string          `json:"billing_interval"`
	AmountVND     int64             `json:"amount_vnd"`
	Currency      string            `json:"currency"`
	CheckoutURL   string            `json:"checkout_url"`
	FormFields    map[string]string `json:"form_fields"`
	Env           string            `json:"env"`
}
