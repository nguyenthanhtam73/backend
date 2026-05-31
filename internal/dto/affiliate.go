package dto

import (
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// LogAffiliateClickRequest is POST /affiliate/clicks body.
type LogAffiliateClickRequest struct {
	ProductName   string `json:"product_name"`
	Brand         string `json:"brand,omitempty"`
	AffiliateLink string `json:"affiliate_link"`
	Source        string `json:"source"`
	ContextID     string `json:"context_id,omitempty"`
	PriceRange    string `json:"price_range,omitempty"`
	Priority      string `json:"priority,omitempty"`
}

// AffiliateClickResponse is returned after logging a click.
type AffiliateClickResponse struct {
	ID        string `json:"id"`
	LoggedAt  string `json:"logged_at"`
}

// ValidateAndMap checks the request and maps to a domain row.
func (r LogAffiliateClickRequest) ValidateAndMap(userID uuid.UUID) (*domain.AffiliateClick, string) {
	if userID == uuid.Nil {
		return nil, "user id required"
	}
	name := strings.TrimSpace(r.ProductName)
	link := strings.TrimSpace(r.AffiliateLink)
	source := strings.TrimSpace(r.Source)
	if name == "" {
		return nil, "product_name is required"
	}
	if link == "" {
		return nil, "affiliate_link is required"
	}
	if !domain.IsValidAffiliateClickSource(source) {
		return nil, "invalid source"
	}
	return &domain.AffiliateClick{
		UserID:        userID,
		ProductName:   name,
		Brand:         strings.TrimSpace(r.Brand),
		AffiliateLink: link,
		Source:        source,
		ContextID:     strings.TrimSpace(r.ContextID),
		PriceRange:    strings.TrimSpace(r.PriceRange),
		Priority:      strings.TrimSpace(r.Priority),
	}, ""
}
