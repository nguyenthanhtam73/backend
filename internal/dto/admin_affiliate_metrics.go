package dto

// AdminAffiliateSKURow is one aggregated affiliate SKU in admin metrics.
type AdminAffiliateSKURow struct {
	ProductID     string `json:"product_id,omitempty"`
	ProductName   string `json:"product_name"`
	Brand         string `json:"brand,omitempty"`
	AffiliateLink string `json:"affiliate_link,omitempty"`
	Clicks7d      int64  `json:"clicks_7d"`
	Clicks30d     int64  `json:"clicks_30d"`
	ClicksTotal   int64  `json:"clicks_total"`
	LastClickAt   string `json:"last_click_at,omitempty"` // RFC3339
}

// AdminAffiliateMetricsResponse is GET /admin/metrics/affiliate.
type AdminAffiliateMetricsResponse struct {
	Clicks7d   int64                   `json:"clicks_7d"`
	Clicks30d  int64                   `json:"clicks_30d"`
	ClicksTotal int64                  `json:"clicks_total"`
	TopSKUs    []AdminAffiliateSKURow  `json:"top_skus"`
	AsOf       string                  `json:"as_of"`
}
