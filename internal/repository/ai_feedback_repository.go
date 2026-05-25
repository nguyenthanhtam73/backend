package repository

import (
	"context"
	"fmt"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormAIFeedbackRepository stores user ratings on AI output.
type GormAIFeedbackRepository struct {
	db *gorm.DB
}

// NewAIFeedbackRepository constructs the repository.
func NewAIFeedbackRepository(db *gorm.DB) *GormAIFeedbackRepository {
	return &GormAIFeedbackRepository{db: db}
}

func (r *GormAIFeedbackRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts a feedback row (append-only log).
func (r *GormAIFeedbackRepository) Create(ctx context.Context, row *domain.AIUserFeedback) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(row).Error
}

// ListByUser returns up to `limit` most recent feedback rows for the user
// (newest first). limit <= 0 falls back to a sensible default (50). Used by
// GET /ai/feedback/me and by the AI prompt loop to inject prior feedback.
func (r *GormAIFeedbackRepository) ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]domain.AIUserFeedback, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var rows []domain.AIUserFeedback
	q := db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit)
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
