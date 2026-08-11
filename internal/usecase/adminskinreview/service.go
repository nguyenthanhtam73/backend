// Package adminskinreview handles admin-only deep skin observation (no routine).
package adminskinreview

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/platform/imgprep"
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

const publicSlugAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
const publicSlugLength = 10

// UploadImage is one validated photo ready to persist + send to vision.
type UploadImage struct {
	Rel         string
	Data        []byte
	ContentType string
}

// CreateInput is the analyze (+ persist) request from the admin handler.
type CreateInput struct {
	Title        string
	Notes        string
	UserQuestion string
	Answer       string
	Status       string
	Locale       string
	Images       []UploadImage
}

const (
	maxUserQuestionRunes = 2000
	maxAnswerRunes       = 4000
)

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
	if len(in.Images) < 1 {
		return zero, fmt.Errorf("%w: upload 1 skin photo (required)", ErrInvalidInput)
	}
	if len(in.Images) > 3 {
		return zero, fmt.Errorf("%w: maximum 3 images", ErrInvalidInput)
	}

	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = string(domain.AdminSkinReviewStatusDraft)
	}
	if !domain.IsValidAdminSkinReviewStatus(status) {
		return zero, fmt.Errorf("%w: status must be draft or published", ErrInvalidInput)
	}
	locale := dto.NormalizeAdminSkinReviewLocale(in.Locale)
	userQuestion, err := clampAdminSkinText(in.UserQuestion, maxUserQuestionRunes)
	if err != nil {
		return zero, err
	}
	answer, err := clampAdminSkinText(in.Answer, maxAnswerRunes)
	if err != nil {
		return zero, err
	}

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

	analysis, modelUsed, err := ai.AdminSkinReviewAnalyze(ctx, s.cfg, s.httpClient, aiImgs, locale, userQuestion)
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
		AdminUserID:      adminUserID,
		Title:            strings.TrimSpace(in.Title),
		Notes:            strings.TrimSpace(in.Notes),
		UserQuestion:     userQuestion,
		Answer:           answer,
		Status:           status,
		ImagePaths:       pathsJSON,
		PublicImagePaths: json.RawMessage("[]"),
		Analysis:         analysisJSON,
		Locale:           locale,
		ModelUsed:        modelUsed,
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

// Patch updates title / notes / user_question / answer / status on an existing review.
func (s *Service) Patch(
	ctx context.Context,
	id uuid.UUID,
	req dto.PatchAdminSkinReviewRequest,
) (dto.AdminSkinReviewResponse, error) {
	var zero dto.AdminSkinReviewResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	if req.Title == nil && req.Notes == nil && req.UserQuestion == nil && req.Answer == nil && req.Status == nil {
		return zero, fmt.Errorf("%w: provide title, notes, user_question, answer, and/or status", ErrInvalidInput)
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
	if req.UserQuestion != nil {
		q, err := clampAdminSkinText(*req.UserQuestion, maxUserQuestionRunes)
		if err != nil {
			return zero, err
		}
		req.UserQuestion = &q
	}
	if req.Answer != nil {
		a, err := clampAdminSkinText(*req.Answer, maxAnswerRunes)
		if err != nil {
			return zero, err
		}
		req.Answer = &a
	}

	// Public rows must keep the Q⇒A invariant (drafts may have question before answer).
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return zero, err
	}
	if existing == nil {
		return zero, ErrNotFound
	}
	if existing.IsPublic {
		nextQ := existing.UserQuestion
		nextA := existing.Answer
		if req.UserQuestion != nil {
			nextQ = *req.UserQuestion
		}
		if req.Answer != nil {
			nextA = *req.Answer
		}
		if err := requireAnswerIfQuestion(nextQ, nextA); err != nil {
			return zero, err
		}
	}

	row, err := s.repo.UpdateMeta(ctx, id, req.Title, req.Notes, req.UserQuestion, req.Answer, req.Status)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, ErrNotFound
	}
	rels, _ := dto.DecodeStringSlice(row.ImagePaths)
	return dto.FromDomainAdminSkinReview(row, publicUploadURLs(rels)), nil
}

// Reanalyze re-runs vision on saved images using the current (or override) user_question
// so the public analysis block picks up context typed after the first Create.
func (s *Service) Reanalyze(
	ctx context.Context,
	id uuid.UUID,
	req dto.ReanalyzeAdminSkinReviewRequest,
) (dto.AdminSkinReviewResponse, error) {
	var zero dto.AdminSkinReviewResponse
	if s == nil || s.repo == nil || s.store == nil || s.cfg == nil {
		return zero, ErrUnavailable
	}
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, ErrNotFound
	}

	question := strings.TrimSpace(row.UserQuestion)
	// Only non-empty overrides win — empty "" must not wipe a saved question.
	if req.UserQuestion != nil {
		if q := strings.TrimSpace(*req.UserQuestion); q != "" {
			q, cerr := clampAdminSkinText(q, maxUserQuestionRunes)
			if cerr != nil {
				return zero, cerr
			}
			question = q
			if q != strings.TrimSpace(row.UserQuestion) {
				patched, perr := s.repo.UpdateMeta(ctx, id, nil, nil, &q, nil, nil)
				if perr != nil {
					return zero, perr
				}
				if patched != nil {
					row = patched
				}
			}
		}
	}

	rels, _ := dto.DecodeStringSlice(row.ImagePaths)
	if len(rels) == 0 {
		return zero, fmt.Errorf("%w: no images to reanalyze", ErrInvalidInput)
	}
	aiImgs := make([]ai.ImageBytes, 0, len(rels))
	for _, rel := range rels {
		raw, rerr := s.store.Read(ctx, rel)
		if rerr != nil {
			return zero, fmt.Errorf("%w: read image: %v", ErrAnalysis, rerr)
		}
		if len(raw) == 0 {
			continue
		}
		aiImgs = append(aiImgs, ai.ImageBytes{Data: raw})
	}
	if len(aiImgs) == 0 {
		return zero, fmt.Errorf("%w: no readable images", ErrAnalysis)
	}

	analysis, modelUsed, err := ai.AdminSkinReviewAnalyze(ctx, s.cfg, s.httpClient, aiImgs, row.Locale, question)
	if err != nil {
		return zero, fmt.Errorf("%w: %v", ErrAnalysis, err)
	}
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return zero, fmt.Errorf("marshal analysis: %w", err)
	}
	row, err = s.repo.UpdateAnalysis(ctx, id, analysisJSON, modelUsed)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, ErrNotFound
	}
	outRels, _ := dto.DecodeStringSlice(row.ImagePaths)
	return dto.FromDomainAdminSkinReview(row, publicUploadURLs(outRels)), nil
}

// SuggestAnswer drafts a public reply from user_question + saved analysis (admin may edit).
// When refresh_analysis=true, re-runs vision with the question first.
// Always aligns public tips/laterality with the question and persists if changed.
func (s *Service) SuggestAnswer(
	ctx context.Context,
	id uuid.UUID,
	req dto.SuggestAdminSkinReviewAnswerRequest,
) (dto.SuggestAdminSkinReviewAnswerResponse, error) {
	var zero dto.SuggestAdminSkinReviewAnswerResponse
	if s == nil || s.repo == nil || s.cfg == nil {
		return zero, ErrUnavailable
	}

	refresh := req.RefreshAnalysis != nil && *req.RefreshAnalysis
	if refresh {
		reReq := dto.ReanalyzeAdminSkinReviewRequest{UserQuestion: req.UserQuestion}
		if _, err := s.Reanalyze(ctx, id, reReq); err != nil {
			return zero, err
		}
	}

	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, ErrNotFound
	}

	question := strings.TrimSpace(row.UserQuestion)
	// Only non-empty overrides win — empty JSON "" must not wipe a saved question.
	if req.UserQuestion != nil {
		if q := strings.TrimSpace(*req.UserQuestion); q != "" {
			question = q
			if !refresh && q != strings.TrimSpace(row.UserQuestion) {
				if _, perr := s.repo.UpdateMeta(ctx, id, nil, nil, &q, nil, nil); perr != nil {
					return zero, perr
				}
				row, _ = s.repo.GetByID(ctx, id)
				if row == nil {
					return zero, ErrNotFound
				}
			}
		}
	}
	if question == "" {
		return zero, fmt.Errorf("%w: user_question required to suggest an answer", ErrInvalidInput)
	}
	if _, err := clampAdminSkinText(question, maxUserQuestionRunes); err != nil {
		return zero, err
	}

	analysis := dto.AdminSkinReviewAnalysis{}
	if len(row.Analysis) > 0 {
		_ = json.Unmarshal(row.Analysis, &analysis)
	}
	dto.NormalizeAdminSkinReviewAnalysis(&analysis, row.Locale)

	aligned := ai.AlignAdminSkinAnalysisWithQuestion(&analysis, question, row.Locale)
	var analysisOut *dto.AdminSkinReviewAnalysis
	if aligned {
		analysisJSON, merr := json.Marshal(analysis)
		if merr != nil {
			return zero, fmt.Errorf("marshal analysis: %w", merr)
		}
		if _, uerr := s.repo.UpdateAnalysis(ctx, id, analysisJSON, ""); uerr != nil {
			return zero, uerr
		}
	}
	// Return analysis when refreshed or aligned so the admin UI can update the notes block.
	if aligned || refresh {
		cp := analysis
		analysisOut = &cp
	}

	draft, err := ai.AdminSkinReviewSuggestAnswer(ctx, s.cfg, s.httpClient, question, &analysis, row.Locale)
	if err != nil {
		return zero, fmt.Errorf("%w: %v", ErrAnalysis, err)
	}
	draft, err = clampAdminSkinText(draft, maxAnswerRunes)
	if err != nil {
		return zero, err
	}
	if draft == "" {
		return zero, fmt.Errorf("%w: empty suggested answer", ErrAnalysis)
	}
	return dto.SuggestAdminSkinReviewAnswerResponse{Answer: draft, Analysis: analysisOut}, nil
}

// Publish generates a unique public slug, privacy-blurs images, and marks the review public.
// Idempotent: if already public with a slug, returns the existing share payload
// (re-blurs only when public_image_paths is empty).
func (s *Service) Publish(ctx context.Context, id uuid.UUID) (dto.AdminSkinReviewResponse, error) {
	var zero dto.AdminSkinReviewResponse
	if s == nil || s.repo == nil || s.store == nil {
		return zero, ErrUnavailable
	}
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, ErrNotFound
	}
	if err := requireAnswerIfQuestion(row.UserQuestion, row.Answer); err != nil {
		return zero, err
	}

	slug := strings.TrimSpace(row.PublicSlug)
	if slug == "" {
		slug, err = s.uniquePublicSlug(ctx)
		if err != nil {
			return zero, err
		}
	}

	blurRels, _ := dto.DecodeStringSlice(row.PublicImagePaths)
	if len(blurRels) == 0 {
		origRels, _ := dto.DecodeStringSlice(row.ImagePaths)
		if len(origRels) == 0 {
			return zero, fmt.Errorf("%w: no images to publish", ErrInvalidInput)
		}
		blurRels = make([]string, 0, len(origRels))
		for i, rel := range origRels {
			raw, rerr := s.store.Read(ctx, rel)
			if rerr != nil {
				return zero, fmt.Errorf("read image for blur: %w", rerr)
			}
			blurred, berr := imgprep.SoftBlurForShare(raw)
			if berr != nil {
				return zero, fmt.Errorf("blur image: %w", berr)
			}
			blurRel := pathJoin(
				row.AdminUserID.String(),
				"admin-skin-review",
				"public",
				slug,
				fmt.Sprintf("%d.jpg", i),
			)
			if err := s.store.Save(ctx, blurRel, blurred, "image/jpeg"); err != nil {
				return zero, fmt.Errorf("save blurred image: %w", err)
			}
			blurRels = append(blurRels, blurRel)
		}
	}

	pathsJSON, err := json.Marshal(blurRels)
	if err != nil {
		return zero, err
	}
	publishedAt := time.Now().UTC()
	if row.PublishedAt != nil {
		publishedAt = *row.PublishedAt
	}

	updated, err := s.repo.PublishFields(ctx, id, slug, pathsJSON, publishedAt)
	if err != nil {
		return zero, err
	}
	if updated == nil {
		return zero, ErrNotFound
	}
	origRels, _ := dto.DecodeStringSlice(updated.ImagePaths)
	return dto.FromDomainAdminSkinReview(updated, publicUploadURLs(origRels)), nil
}

// List returns paginated reviews for the admin console.
func (s *Service) List(
	ctx context.Context,
	filter repository.AdminSkinReviewListFilter,
) (dto.AdminSkinReviewListResponse, error) {
	var zero dto.AdminSkinReviewListResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	rows, total, err := s.repo.ListAdmin(ctx, filter)
	if err != nil {
		return zero, err
	}
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	out := dto.AdminSkinReviewListResponse{
		Items:    make([]dto.AdminSkinReviewListItem, 0, len(rows)),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}
	for _, row := range rows {
		out.Items = append(out.Items, dto.FromDomainAdminSkinReviewListItem(row))
	}
	return out, nil
}

// Unpublish removes public visibility (admin-only).
func (s *Service) Unpublish(ctx context.Context, id uuid.UUID) (dto.AdminSkinReviewResponse, error) {
	var zero dto.AdminSkinReviewResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	row, err := s.repo.UnpublishFields(ctx, id)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, ErrNotFound
	}
	rels, _ := dto.DecodeStringSlice(row.ImagePaths)
	return dto.FromDomainAdminSkinReview(row, publicUploadURLs(rels)), nil
}

// ListPublicForSitemap returns public share slugs for frontend sitemap.xml.
func (s *Service) ListPublicForSitemap(ctx context.Context, limit int) (dto.PublicSkinReviewSitemapResponse, error) {
	var zero dto.PublicSkinReviewSitemapResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	rows, err := s.repo.ListPublicForSitemap(ctx, limit)
	if err != nil {
		return zero, err
	}
	out := dto.PublicSkinReviewSitemapResponse{
		Items: make([]dto.PublicSkinReviewSitemapItem, 0, len(rows)),
	}
	for _, row := range rows {
		item := dto.PublicSkinReviewSitemapItem{
			Slug:      row.Slug,
			UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if row.PublishedAt != nil {
			item.PublishedAt = row.PublishedAt.UTC().Format(time.RFC3339)
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// GetPublic returns the share payload for a public slug (no admin notes, blurred images only).
func (s *Service) GetPublic(ctx context.Context, slug string) (dto.PublicSkinReviewResponse, error) {
	var zero dto.PublicSkinReviewResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" || !isValidPublicSlug(slug) {
		return zero, ErrNotFound
	}
	row, err := s.repo.GetByPublicSlug(ctx, slug)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, ErrNotFound
	}
	blurRels, _ := dto.DecodeStringSlice(row.PublicImagePaths)
	return dto.FromDomainPublicSkinReview(row, publicUploadURLs(blurRels)), nil
}

func (s *Service) uniquePublicSlug(ctx context.Context) (string, error) {
	for i := 0; i < 12; i++ {
		slug, err := randomPublicSlug(publicSlugLength)
		if err != nil {
			return "", err
		}
		exists, err := s.repo.ExistsPublicSlug(ctx, slug)
		if err != nil {
			return "", err
		}
		if !exists {
			return slug, nil
		}
	}
	return "", fmt.Errorf("could not allocate unique public slug")
}

func randomPublicSlug(n int) (string, error) {
	var b strings.Builder
	b.Grow(n)
	max := big.NewInt(int64(len(publicSlugAlphabet)))
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(publicSlugAlphabet[v.Int64()])
	}
	return b.String(), nil
}

func isValidPublicSlug(slug string) bool {
	if len(slug) < 6 || len(slug) > 32 {
		return false
	}
	for _, r := range slug {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
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

func pathJoin(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.ReplaceAll(p, "\\", "/"), "/")
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return strings.Join(cleaned, "/")
}

func clampAdminSkinText(raw string, maxRunes int) (string, error) {
	s := strings.TrimSpace(raw)
	if maxRunes <= 0 {
		return s, nil
	}
	runes := []rune(s)
	if len(runes) > maxRunes {
		return "", fmt.Errorf("%w: text exceeds %d characters", ErrInvalidInput, maxRunes)
	}
	return s, nil
}

// requireAnswerIfQuestion enforces: public FB question must ship with a reply.
// Drafts may store a question alone until the admin drafts/saves an answer.
func requireAnswerIfQuestion(userQuestion, answer string) error {
	if strings.TrimSpace(userQuestion) != "" && strings.TrimSpace(answer) == "" {
		return fmt.Errorf("%w: answer required when user_question is set", ErrInvalidInput)
	}
	return nil
}
