package repository

import (
	"context"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RefreshSessionRepository persists refresh JWT sessions for revoke / rotation.
type RefreshSessionRepository interface {
	Create(ctx context.Context, session *domain.RefreshSession) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.RefreshSession, error)
	RevokeByID(ctx context.Context, id uuid.UUID, at time.Time) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error
}

// GormRefreshSessionRepository is the Postgres adapter.
type GormRefreshSessionRepository struct {
	db *gorm.DB
}

// NewRefreshSessionRepository constructs the GORM adapter.
func NewRefreshSessionRepository(db *gorm.DB) *GormRefreshSessionRepository {
	return &GormRefreshSessionRepository{db: db}
}

func (r *GormRefreshSessionRepository) Create(ctx context.Context, session *domain.RefreshSession) error {
	if r == nil || r.db == nil || session == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *GormRefreshSessionRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.RefreshSession, error) {
	if r == nil || r.db == nil || id == uuid.Nil {
		return nil, nil
	}
	var row domain.RefreshSession
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *GormRefreshSessionRepository) RevokeByID(ctx context.Context, id uuid.UUID, at time.Time) error {
	if r == nil || r.db == nil || id == uuid.Nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Model(&domain.RefreshSession{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", at.UTC()).Error
}

func (r *GormRefreshSessionRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error {
	if r == nil || r.db == nil || userID == uuid.Nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Model(&domain.RefreshSession{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", at.UTC()).Error
}
