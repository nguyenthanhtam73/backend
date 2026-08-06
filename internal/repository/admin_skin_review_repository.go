package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormAdminSkinReviewRepository persists admin skin observation sessions.
type GormAdminSkinReviewRepository struct {
	db *gorm.DB
}

// NewAdminSkinReviewRepository constructs the repository.
func NewAdminSkinReviewRepository(db *gorm.DB) *GormAdminSkinReviewRepository {
	return &GormAdminSkinReviewRepository{db: db}
}

func (r *GormAdminSkinReviewRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts a new admin skin review row.
func (r *GormAdminSkinReviewRepository) Create(ctx context.Context, row *domain.AdminSkinReview) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(row).Error
}

// GetByID loads one review by primary key (any admin may read — console is shared).
func (r *GormAdminSkinReviewRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.AdminSkinReview, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("admin skin review id required")
	}
	var row domain.AdminSkinReview
	tx := db.WithContext(ctx).Where("id = ?", id).First(&row)
	if tx.Error != nil {
		if tx.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// GetByPublicSlug loads a publicly shared review (is_public = true).
func (r *GormAdminSkinReviewRepository) GetByPublicSlug(ctx context.Context, slug string) (*domain.AdminSkinReview, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if slug == "" {
		return nil, fmt.Errorf("public slug required")
	}
	var row domain.AdminSkinReview
	tx := db.WithContext(ctx).
		Where("public_slug = ? AND is_public = ?", slug, true).
		First(&row)
	if tx.Error != nil {
		if tx.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// ExistsPublicSlug reports whether slug is already taken (any row).
func (r *GormAdminSkinReviewRepository) ExistsPublicSlug(ctx context.Context, slug string) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	if slug == "" {
		return false, nil
	}
	var n int64
	if err := db.WithContext(ctx).
		Model(&domain.AdminSkinReview{}).
		Where("public_slug = ?", slug).
		Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// UpdateMeta patches title / notes / user_question / answer / status for one review.
func (r *GormAdminSkinReviewRepository) UpdateMeta(
	ctx context.Context,
	id uuid.UUID,
	title *string,
	notes *string,
	userQuestion *string,
	answer *string,
	status *string,
) (*domain.AdminSkinReview, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("admin skin review id required")
	}

	updates := map[string]any{}
	if title != nil {
		updates["title"] = *title
	}
	if notes != nil {
		updates["notes"] = *notes
	}
	if userQuestion != nil {
		updates["user_question"] = *userQuestion
	}
	if answer != nil {
		updates["answer"] = *answer
	}
	if status != nil {
		updates["status"] = *status
	}
	if len(updates) == 0 {
		return r.GetByID(ctx, id)
	}

	res := db.WithContext(ctx).
		Model(&domain.AdminSkinReview{}).
		Where("id = ?", id).
		Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.GetByID(ctx, id)
}

// UpdateAnalysis replaces the stored AI analysis JSON (+ optional model_used).
func (r *GormAdminSkinReviewRepository) UpdateAnalysis(
	ctx context.Context,
	id uuid.UUID,
	analysisJSON []byte,
	modelUsed string,
) (*domain.AdminSkinReview, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("admin skin review id required")
	}
	if len(analysisJSON) == 0 {
		return nil, fmt.Errorf("analysis json required")
	}
	updates := map[string]any{
		"analysis": analysisJSON,
	}
	if strings.TrimSpace(modelUsed) != "" {
		updates["model_used"] = strings.TrimSpace(modelUsed)
	}
	res := db.WithContext(ctx).
		Model(&domain.AdminSkinReview{}).
		Where("id = ?", id).
		Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.GetByID(ctx, id)
}

// AdminSkinReviewListFilter drives admin list pagination + status filter.
type AdminSkinReviewListFilter struct {
	Status   string // draft | published | "" (all)
	Page     int
	PageSize int
}

// ListAdmin returns paginated reviews for the admin console.
func (r *GormAdminSkinReviewRepository) ListAdmin(
	ctx context.Context,
	filter AdminSkinReviewListFilter,
) ([]domain.AdminSkinReview, int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, 0, err
	}
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize

	q := db.WithContext(ctx).Model(&domain.AdminSkinReview{})
	switch filter.Status {
	case string(domain.AdminSkinReviewStatusDraft), string(domain.AdminSkinReviewStatusPublished):
		q = q.Where("status = ?", filter.Status)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []domain.AdminSkinReview
	if err := q.
		Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// PublishFields persists public share fields after blur generation.
func (r *GormAdminSkinReviewRepository) PublishFields(
	ctx context.Context,
	id uuid.UUID,
	slug string,
	publicImagePaths []byte,
	publishedAt time.Time,
) (*domain.AdminSkinReview, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil || slug == "" {
		return nil, fmt.Errorf("id and slug required")
	}
	updates := map[string]any{
		"is_public":          true,
		"public_slug":        slug,
		"published_at":       publishedAt,
		"status":             string(domain.AdminSkinReviewStatusPublished),
		"public_image_paths": publicImagePaths,
	}
	res := db.WithContext(ctx).
		Model(&domain.AdminSkinReview{}).
		Where("id = ?", id).
		Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.GetByID(ctx, id)
}

// UnpublishFields clears public visibility (keeps slug for potential re-publish).
func (r *GormAdminSkinReviewRepository) UnpublishFields(
	ctx context.Context,
	id uuid.UUID,
) (*domain.AdminSkinReview, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("id required")
	}
	updates := map[string]any{
		"is_public":    false,
		"published_at": nil,
		"status":       string(domain.AdminSkinReviewStatusDraft),
	}
	// Select forces zero/NULL values (is_public=false, published_at=NULL) through.
	res := db.WithContext(ctx).
		Model(&domain.AdminSkinReview{}).
		Where("id = ?", id).
		Select("is_public", "published_at", "status").
		Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.GetByID(ctx, id)
}
