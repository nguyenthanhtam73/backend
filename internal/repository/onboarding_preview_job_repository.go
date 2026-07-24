package repository

import (
	"context"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OnboardingPreviewJobRepository persists guest preview starter-routine jobs.
type OnboardingPreviewJobRepository interface {
	Create(ctx context.Context, job *domain.OnboardingPreviewJob) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.OnboardingPreviewJob, error)
	Update(ctx context.Context, job *domain.OnboardingPreviewJob) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// GormOnboardingPreviewJobRepository is the Postgres adapter.
type GormOnboardingPreviewJobRepository struct {
	db *gorm.DB
}

// NewOnboardingPreviewJobRepository constructs the GORM adapter.
func NewOnboardingPreviewJobRepository(db *gorm.DB) *GormOnboardingPreviewJobRepository {
	return &GormOnboardingPreviewJobRepository{db: db}
}

func (r *GormOnboardingPreviewJobRepository) Create(ctx context.Context, job *domain.OnboardingPreviewJob) error {
	if r == nil || r.db == nil || job == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *GormOnboardingPreviewJobRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.OnboardingPreviewJob, error) {
	if r == nil || r.db == nil || id == uuid.Nil {
		return nil, nil
	}
	var row domain.OnboardingPreviewJob
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *GormOnboardingPreviewJobRepository) Update(ctx context.Context, job *domain.OnboardingPreviewJob) error {
	if r == nil || r.db == nil || job == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Save(job).Error
}

func (r *GormOnboardingPreviewJobRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if r == nil || r.db == nil || id == uuid.Nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Delete(&domain.OnboardingPreviewJob{}, "id = ?", id).Error
}

// DeleteExpired removes jobs past ExpiresAt (best-effort housekeeping).
func (r *GormOnboardingPreviewJobRepository) DeleteExpired(ctx context.Context, now time.Time) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).
		Where("expires_at <= ?", now.UTC()).
		Delete(&domain.OnboardingPreviewJob{}).Error
}
