package dto

// ProductGuidanceItem is a product-role recommendation for onboarding / coach.
// When AffiliateProductID is set, ProductName/Brand/AffiliateLink/PriceRange are
// filled from the server catalog only (never hallucinated links).
type ProductGuidanceItem struct {
	Step               string   `json:"step"` // cleanse|moisturize|spf|treat|soothe
	Phase              string   `json:"phase"` // calm_first|can_add_active
	Category           string   `json:"category"`
	NameOrCategory     string   `json:"name_or_category"`
	Why                string   `json:"why"`
	Benefits           []string `json:"benefits"`
	HowToUse           string   `json:"how_to_use"`
	Caution            string   `json:"caution,omitempty"`
	AffiliateProductID string   `json:"affiliate_product_id,omitempty"`
	ProductName        string   `json:"product_name,omitempty"`
	Brand              string   `json:"brand,omitempty"`
	AffiliateLink      string   `json:"affiliate_link,omitempty"`
	PriceRange         string   `json:"price_range,omitempty"`
}
