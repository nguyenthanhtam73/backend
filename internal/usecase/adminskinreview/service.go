// Package adminskinreview handles admin-only deep skin observation (no routine).
package adminskinreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/dadiary/backend/internal/storage"
	"github.com/google/uuid"
)

var (
	ErrUnavailable  = errors.New("admin skin review unavailable")
	ErrNotFound     = errors.New("admin skin review not found")
	ErrInvalidInput = errors.New("invalid admin skin review input")
	ErrAnalysis     = errors.New("admin skin review analysis failed")
)

// UploadImage is one validated photo ready to persist + send to vision.
type UploadImage struct {
	Rel         string
	Data        []byte
	ContentType string
}

// CreateInput is the analyze (+ persist) request from the admin handler.
type CreateInput struct {
	Title  string
	Notes  string
	Status string
	Locale string
	Images []UploadImage
}

// Service orchestrates storage + Premium vision analysis + persistence.
// Intentionally does NOT call premium.AssertFeature / user_usages — admin bypasses Free quotas.
type Service struct {
	repo       *repository.GormAdminSkinReviewRepository
	store      storage.Storage
	cfg        *config.Config
	httpClient *http.Client
}

// NewService constructs Service.
func NewService(
	repo *repository.GormAdminSkinReviewRepository,
	store storage.Storage,
	cfg *config.Config,
) *Service {
	return &Service{
		repo:  repo,
		store: store,
		cfg:   cfg,
		httpClient: &http.Client{
			Timeout: 6 * time.Minute,
		},
	}
}

// Create uploads images, runs observations-only AI, and persists a draft/published row.
func (s *Service) Create(ctx context.Context, adminUserID uuid.UUID, in CreateInput) (dto.AdminSkinReviewResponse, error) {
	var zero dto.AdminSkinReviewResponse
	if s == nil || s.repo == nil || s.store == nil || s.cfg == nil {
		return zero, ErrUnavailable
	}
	if adminUserID == uuid.Nil {
		return zero, fmt.Errorf("%w: admin user required", ErrInvalidInput)
	}
	if len(in.Images) < 1 || len(in.Images) > 3 {
		return zero, fmt.Errorf("%w: upload 1 to 3 images", ErrInvalidInput)
	}

	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = string(domain.AdminSkinReviewStatusDraft)
	}
	if !domain.IsValidAdminSkinReviewStatus(status) {
		return zero, fmt.Errorf("%w: status must be draft or published", ErrInvalidInput)
	}
	locale := dto.NormalizeAdminSkinReviewLocale(in.Locale)

	// Persist photos first so GET can show thumbnails even if AI is slow to return.
	rels := make([]string, 0, len(in.Images))
	aiImgs := make([]ai.ImageBytes, 0, len(in.Images))
	for _, img := range in.Images {
		if len(img.Data) == 0 || strings.TrimSpace(img.Rel) == "" {
			return zero, fmt.Errorf("%w: empty image", ErrInvalidInput)
		}
		if err := s.store.Save(ctx, img.Rel, img.Data, img.ContentType); err != nil {
			return zero, fmt.Errorf("save image: %w", err)
		}
		rels = append(rels, img.Rel)
		aiImgs = append(aiImgs, ai.ImageBytes{Data: img.Data})
	}

	analysis, modelUsed, err := ai.AdminSkinReviewAnalyze(ctx, s.cfg, s.httpClient, aiImgs, locale)
	if err != nil {
		return zero, fmt.Errorf("%w: %v", ErrAnalysis, err)
	}
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return zero, fmt.Errorf("marshal analysis: %w", err)
	}
	pathsJSON, err := json.Marshal(rels)
	if err != nil {
		return zero, fmt.Errorf("marshal image paths: %w", err)
	}

	row := &domain.AdminSkinReview{
		AdminUserID: adminUserID,
		Title:       strings.TrimSpace(in.Title),
		Notes:       strings.TrimSpace(in.Notes),
		Status:      status,
		ImagePaths:  pathsJSON,
		Analysis:    analysisJSON,
		Locale:      locale,
		ModelUsed:   modelUsed,
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return zero, err
	}

	return dto.FromDomainAdminSkinReview(row, publicUploadURLs(rels)), nil
}

// Get returns one saved review by id.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (dto.AdminSkinReviewResponse, error) {
	var zero dto.AdminSkinReviewResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, ErrNotFound
	}
	rels, _ := dto.DecodeStringSlice(row.ImagePaths)
	return dto.FromDomainAdminSkinReview(row, publicUploadURLs(rels)), nil
}

// Patch updates title / notes / status on an existing review.
func (s *Service) Patch(
	ctx context.Context,
	id uuid.UUID,
	req dto.PatchAdminSkinReviewRequest,
) (dto.AdminSkinReviewResponse, error) {
	var zero dto.AdminSkinReviewResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	if req.Title == nil && req.Notes == nil && req.Status == nil {
		return zero, fmt.Errorf("%w: provide title, notes, and/or status", ErrInvalidInput)
	}
	if req.Status != nil {
		st := strings.TrimSpace(*req.Status)
		if !domain.IsValidAdminSkinReviewStatus(st) {
			return zero, fmt.Errorf("%w: status must be draft or published", ErrInvalidInput)
		}
		req.Status = &st
	}
	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		req.Title = &t
	}
	if req.Notes != nil {
		n := strings.TrimSpace(*req.Notes)
		req.Notes = &n
	}

	row, err := s.repo.UpdateMeta(ctx, id, req.Title, req.Notes, req.Status)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, ErrNotFound
	}
	rels, _ := dto.DecodeStringSlice(row.ImagePaths)
	return dto.FromDomainAdminSkinReview(row, publicUploadURLs(rels)), nil
}

func publicUploadURLs(rels []string) []string {
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		clean := strings.TrimLeft(strings.ReplaceAll(rel, "\\", "/"), "/")
		if clean == "" {
			continue
		}
		out = append(out, "/uploads/"+clean)
	}
	return out
}
