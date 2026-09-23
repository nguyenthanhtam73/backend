// Package wardrobe manages the user's skincare product shelf.
package wardrobe

import (
	"context"
	"encoding/json"
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
	ErrNotFound     = errors.New("wardrobe product not found")
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

// Create adds a product owned by the user (Free: up to FreeWardrobeProductLimit).
func (s *Service) Create(ctx context.Context, userID uuid.UUID, req dto.CreateWardrobeProductRequest) (dto.WardrobeProductResponse, error) {
	var zero dto.WardrobeProductResponse
	if s == nil || s.products == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	if s.usage != nil {
		if err := s.usage.AssertWardrobeCreate(ctx, userID); err != nil {
			return zero, err
		}
	}
	name, brand, category, notes, opened, err := normalizeProductFields(req.Name, req.Brand, req.Category, req.Notes, req.OpenedAt)
	if err != nil {
		return zero, err
	}
	p := &domain.SkincareProduct{
		UserID:   userID,
		Name:     name,
		Brand:    brand,
		Category: category,
		Notes:    notes,
		OpenedAt: opened,
	}
	if s.usage != nil {
		paid, err := s.usage.IsPremium(ctx, userID)
		if err != nil {
			return zero, err
		}
		if paid {
			err = s.products.Create(ctx, p)
		} else {
			err = s.products.CreateUnderFreeCap(ctx, p, usageuc.FreeWardrobeProductLimit)
			if errors.Is(err, repository.ErrShelfCapExceeded) {
				return zero, usageuc.ErrQuotaExceeded
			}
		}
		if err != nil {
			return zero, err
		}
	} else if err := s.products.Create(ctx, p); err != nil {
		return zero, err
	}
	if s.cache != nil {
		s.cache.Bust(userID)
	}
	return dto.WardrobeProductFromDomain(p), nil
}

// Update edits a product owned by the user (any plan; ownership checked in repo).
func (s *Service) Update(
	ctx context.Context,
	userID, productID uuid.UUID,
	req dto.UpdateWardrobeProductRequest,
) (dto.WardrobeProductResponse, error) {
	var zero dto.WardrobeProductResponse
	if s == nil || s.products == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	if productID == uuid.Nil {
		return zero, ErrNotFound
	}
	if s.usage != nil {
		if err := s.usage.AssertWardrobeManage(ctx, userID); err != nil {
			return zero, err
		}
	}
	name, brand, category, notes, opened, err := normalizeProductFields(req.Name, req.Brand, req.Category, req.Notes, req.OpenedAt)
	if err != nil {
		return zero, err
	}
	p, err := s.products.GetByIDForUser(ctx, userID, productID)
	if err != nil {
		return zero, err
	}
	if p == nil {
		return zero, ErrNotFound
	}
	identityChanged := shelfIdentityChanged(p, name, brand, category, notes)
	p.Name = name
	p.Brand = brand
	p.Category = category
	p.Notes = notes
	p.OpenedAt = opened
	if identityChanged {
		p.Insight = nil
		p.InsightAt = nil
	}
	if err := s.products.Update(ctx, p); err != nil {
		return zero, err
	}
	if identityChanged {
		if err := s.products.ClearInsight(ctx, userID, productID); err != nil {
			return zero, err
		}
	}
	if s.cache != nil {
		s.cache.Bust(userID)
	}
	return dto.WardrobeProductFromDomain(p), nil
}

// Delete soft-deletes a product owned by the user (any plan; frees a Free create slot).
func (s *Service) Delete(ctx context.Context, userID, productID uuid.UUID) error {
	if s == nil || s.products == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	if productID == uuid.Nil {
		return ErrNotFound
	}
	if s.usage != nil {
		if err := s.usage.AssertWardrobeManage(ctx, userID); err != nil {
			return err
		}
	}
	ok, err := s.products.SoftDelete(ctx, userID, productID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if s.cache != nil {
		s.cache.Bust(userID)
	}
	return nil
}

// AssertCanCreate checks Free shelf slots / Premium write access without creating a row.
func (s *Service) AssertCanCreate(ctx context.Context, userID uuid.UUID) error {
	if s == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	if s.usage == nil {
		return nil
	}
	return s.usage.AssertWardrobeCreate(ctx, userID)
}

// GetOwned loads one product the user owns.
func (s *Service) GetOwned(ctx context.Context, userID, productID uuid.UUID) (*domain.SkincareProduct, error) {
	if s == nil || s.products == nil {
		return nil, fmt.Errorf("%w", ErrUnavailable)
	}
	if productID == uuid.Nil {
		return nil, ErrNotFound
	}
	p, err := s.products.GetByIDForUser(ctx, userID, productID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNotFound
	}
	return p, nil
}

// SaveInsight stores a normalized cabinet card on the product.
func (s *Service) SaveInsight(
	ctx context.Context,
	userID, productID uuid.UUID,
	insight dto.WardrobeProductInsight,
) (dto.WardrobeProductResponse, error) {
	var zero dto.WardrobeProductResponse
	if s == nil || s.products == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	if strings.TrimSpace(insight.WhatItDoes) == "" {
		return zero, fmt.Errorf("%w: insight missing what the product does", ErrInvalidInput)
	}
	insight.Disclaimer = dto.WardrobeInsightDisclaimer
	p, err := s.GetOwned(ctx, userID, productID)
	if err != nil {
		return zero, err
	}
	raw, err := json.Marshal(insight)
	if err != nil {
		return zero, err
	}
	now := time.Now().UTC()
	ok, err := s.products.SetInsight(ctx, userID, productID, raw, now)
	if err != nil {
		return zero, err
	}
	if !ok {
		return zero, ErrNotFound
	}
	p.Insight = raw
	p.InsightAt = &now
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

func shelfIdentityChanged(p *domain.SkincareProduct, name, brand, category, notes string) bool {
	if p == nil {
		return false
	}
	return p.Name != name || p.Brand != brand || p.Category != category || p.Notes != notes
}

func normalizeProductFields(
	nameRaw, brandRaw, categoryRaw, notesRaw, openedRaw string,
) (name, brand, category, notes string, opened *time.Time, err error) {
	name = strings.TrimSpace(nameRaw)
	if name == "" {
		return "", "", "", "", nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	brand = strings.TrimSpace(brandRaw)
	category = strings.TrimSpace(categoryRaw)
	notes = strings.TrimSpace(notesRaw)
	opened, err = parseOpenedAt(openedRaw)
	if err != nil {
		return "", "", "", "", nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return name, brand, category, notes, opened, nil
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
