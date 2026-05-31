package dto

// ProductSuggestion is a curated affiliate pick surfaced by the AI coach.
// Links are validated server-side against the affiliate catalog before returning to clients.
type ProductSuggestion struct {
	ProductName   string `json:"product_name"`
	Brand         string `json:"brand"`
	Reason        string `json:"reason"`
	AffiliateLink string `json:"affiliate_link"`
	PriceRange    string `json:"price_range"`
	Priority      string `json:"priority"` // high | medium
}
