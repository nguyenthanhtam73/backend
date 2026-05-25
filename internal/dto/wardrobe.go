package dto

import (
	"time"

	"github.com/dadiary/backend/internal/domain"
)

// CreateWardrobeProductRequest is POST /wardrobe/products body.
type CreateWardrobeProductRequest struct {
	Name     string `json:"name"`
	Brand    string `json:"brand,omitempty"`
	Category string `json:"category,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

// WardrobeProductResponse is one item in GET /wardrobe.
type WardrobeProductResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Brand     string `json:"brand,omitempty"`
	Category  string `json:"category,omitempty"`
	Notes     string `json:"notes,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// WardrobeListResponse wraps the cabinet list.
type WardrobeListResponse struct {
	Products []WardrobeProductResponse `json:"products"`
}

// WardrobeProductFromDomain maps domain row to API.
func WardrobeProductFromDomain(p *domain.SkincareProduct) WardrobeProductResponse {
	if p == nil {
		return WardrobeProductResponse{}
	}
	return WardrobeProductResponse{
		ID:        p.ID.String(),
		UserID:    p.UserID.String(),
		Name:      p.Name,
		Brand:     p.Brand,
		Category:  p.Category,
		Notes:     p.Notes,
		CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
