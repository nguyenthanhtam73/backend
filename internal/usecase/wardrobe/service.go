// Package wardrobe manages the user's skincare product shelf.
package wardrobe

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrInvalidInput = errors.New("invalid product payload")
	ErrUnavailable  = errors.New("wardrobe service unavailable")
)

// Service handles product CRUD for the wardrobe API.
type Service struct {
	products *repository.GormSkincareProductRepository
}

// NewService wires dependencies.
func NewService(products *repository.GormSkincareProductRepository) *Service {
	return &Service{products: products}
}

// Create adds a product owned by the user.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, req dto.CreateWardrobeProductRequest) (dto.WardrobeProductResponse, error) {
	var zero dto.WardrobeProductResponse
	if s == nil || s.products == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return zero, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	p := &domain.SkincareProduct{
		UserID:   userID,
		Name:     name,
		Brand:    strings.TrimSpace(req.Brand),
		Category: strings.TrimSpace(req.Category),
		Notes:    strings.TrimSpace(req.Notes),
	}
	if err := s.products.Create(ctx, p); err != nil {
		return zero, err
	}
	return dto.WardrobeProductFromDomain(p), nil
}

// List returns the user's products (newest first).
func (s *Service) List(ctx context.Context, userID uuid.UUID) (dto.WardrobeListResponse, error) {
	var out dto.WardrobeListResponse
	if s == nil || s.products == nil {
		return out, fmt.Errorf("%w", ErrUnavailable)
	}
	rows, err := s.products.ListByUser(ctx, userID)
	if err != nil {
		return out, err
	}
	out.Products = make([]dto.WardrobeProductResponse, 0, len(rows))
	for i := range rows {
		out.Products = append(out.Products, dto.WardrobeProductFromDomain(&rows[i]))
	}
	return out, nil
}
