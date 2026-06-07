// Package wardrobe manages the user's skincare product shelf.
package wardrobe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	usageuc "github.com/dadiary/backend/internal/usecase/usage"
	"github.com/google/uuid"
)

var (
	ErrInvalidInput = errors.New("invalid product payload")
	ErrUnavailable  = errors.New("wardrobe service unavailable")
)

// Service handles product CRUD for the wardrobe API.
type Service struct {
	products *repository.GormSkincareProductRepository
	cache    *ai.MemoryCache
	usage    *usageuc.Service
}

// NewService wires dependencies. cache and usage may be nil.
func NewService(
	products *repository.GormSkincareProductRepository,
	cache *ai.MemoryCache,
	usage *usageuc.Service,
) *Service {
	return &Service{products: products, cache: cache, usage: usage}
}

// Create adds a product owned by the user.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, req dto.CreateWardrobeProductRequest) (dto.WardrobeProductResponse, error) {
	var zero dto.WardrobeProductResponse
	if s == nil || s.products == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	if s.usage != nil {
		if err := s.usage.AssertWardrobeWrite(ctx, userID); err != nil {
			return zero, err
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return zero, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	brand := strings.TrimSpace(req.Brand)
	if brand == "" {
		return zero, fmt.Errorf("%w: brand is required", ErrInvalidInput)
	}
	p := &domain.SkincareProduct{
		UserID:   userID,
		Name:     name,
		Brand:    brand,
		Category: strings.TrimSpace(req.Category),
		Notes:    strings.TrimSpace(req.Notes),
	}
	if opened, err := parseOpenedAt(req.OpenedAt); err != nil {
		return zero, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	} else if opened != nil {
		p.OpenedAt = opened
	}
	if err := s.products.Create(ctx, p); err != nil {
		return zero, err
	}
	if s.cache != nil {
		s.cache.Bust(userID)
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

func parseOpenedAt(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, fmt.Errorf("opened_at must be YYYY-MM-DD")
	}
	utc := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &utc, nil
}
