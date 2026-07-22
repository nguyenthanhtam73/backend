package dto

// AdminPaymentMetricsResponse is GET /api/v1/admin/metrics/payment.
type AdminPaymentMetricsResponse struct {
	// TodayPayments is checkout orders created today (UTC).
	TodayPayments int64 `json:"today_payments"`
	// SuccessRate is paid / (paid + failed terminal) for today, 0–100. Pending excluded.
	SuccessRate float64 `json:"success_rate"`
	// TotalRevenue is sum of amount_vnd for paid orders created today (VND).
	TotalRevenue int64 `json:"total_revenue"`
	// FailedCount is failed + cancelled + expired orders created today.
	FailedCount int64 `json:"failed_count"`
	// WebhookErrorsLast24h counts persisted webhook_error ops events.
	WebhookErrorsLast24h int64 `json:"webhook_errors_last_24h"`
	// ActivePremiumCount is users with plan_tier premium / premium_plus.
	ActivePremiumCount int64 `json:"active_premium_count"`
	// UpcomingExpiries lists paid plans ending within the next 7 days.
	UpcomingExpiries []AdminUpcomingExpiry `json:"upcoming_expiries"`
	// RecentPayments is the newest orders (optional status filter via query).
	RecentPayments []AdminPaymentOrderRow `json:"recent_payments"`
	// RecentPaymentsTotal is the filtered total for pagination.
	RecentPaymentsTotal int64 `json:"recent_payments_total"`
	// AsOf is the UTC timestamp when metrics were computed.
	AsOf string `json:"as_of"`
}

// AdminUpcomingExpiry is one row in upcoming_expiries.
type AdminUpcomingExpiry struct {
	UserID        string `json:"user_id"`
	Email         string `json:"email"`
	Plan          string `json:"plan"`
	PlanExpiresAt string `json:"plan_expires_at"`
}

// AdminPaymentOrderRow is one row in recent_payments.
type AdminPaymentOrderRow struct {
	ID              string `json:"id"`
	UserID          string `json:"user_id"`
	InvoiceNumber   string `json:"invoice_number"`
	Plan            string `json:"plan"`
	BillingInterval string `json:"billing_interval"`
	AmountVND       int64  `json:"amount_vnd"`
	Status          string `json:"status"`
	Provider        string `json:"provider"`
	PaidAt          string `json:"paid_at,omitempty"`
	CreatedAt       string `json:"created_at"`
}
