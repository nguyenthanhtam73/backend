package repository

import (
	"context"
	"errors"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormRoutineSuggestJobRepository persists async AI routine-suggest jobs.
type GormRoutineSuggestJobRepository struct {
	db *gorm.DB
}

func NewRoutineSuggestJobRepository(db *gorm.DB) *GormRoutineSuggestJobRepository {
	return &GormRoutineSuggestJobRepository{db: db}
}

func (r *GormRoutineSuggestJobRepository) Create(ctx context.Context, job *domain.RoutineSuggestJob) error {
	if r == nil || r.db == nil || job == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *GormRoutineSuggestJobRepository) GetByIDForUser(
	ctx context.Context,
	userID, id uuid.UUID,
) (*domain.RoutineSuggestJob, error) {
	if r == nil || r.db == nil || userID == uuid.Nil || id == uuid.Nil {
		return nil, nil
	}
	var row domain.RoutineSuggestJob
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *GormRoutineSuggestJobRepository) Update(ctx context.Context, job *domain.RoutineSuggestJob) error {
	if r == nil || r.db == nil || job == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Save(job).Error
}

// MarkTerminal sets status only when the job is still processing (avoids
// overwriting a user cancel with a late AI result).
func (r *GormRoutineSuggestJobRepository) MarkTerminal(
	ctx context.Context,
	userID, id uuid.UUID,
	status domain.RoutineSuggestJobStatus,
	result []byte,
	errMsg string,
) (bool, error) {
	if r == nil || r.db == nil || userID == uuid.Nil || id == uuid.Nil {
		return false, gorm.ErrInvalidDB
	}
	updates := map[string]any{
		"status":     status,
		"error_msg":  errMsg,
		"updated_at": time.Now().UTC(),
	}
	if len(result) > 0 {
		updates["result"] = result
	}
	res := r.db.WithContext(ctx).
		Model(&domain.RoutineSuggestJob{}).
		Where("id = ? AND user_id = ? AND status = ?", id, userID, domain.SuggestJobProcessing).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
