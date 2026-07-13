package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// FeedbackListFilter drives admin list pagination and optional filters.
type FeedbackListFilter struct {
	Type     string
	Status   string
	Page     int
	PageSize int
}

// GormFeedbackRepository persists user-submitted product feedback.
type GormFeedbackRepository struct {
	db *gorm.DB
}

// NewFeedbackRepository constructs the repository.
func NewFeedbackRepository(db *gorm.DB) *GormFeedbackRepository {
	return &GormFeedbackRepository{db: db}
}

func (r *GormFeedbackRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts a feedback row.
func (r *GormFeedbackRepository) Create(ctx context.Context, row *domain.Feedback) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(row).Error
}

// ListAdmin returns paginated feedback rows with user email/username preloaded.
func (r *GormFeedbackRepository) ListAdmin(
	ctx context.Context,
	filter FeedbackListFilter,
) ([]domain.Feedback, int64, error) {
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

	q := db.WithContext(ctx).Model(&domain.Feedback{})
	if typ := strings.TrimSpace(filter.Type); typ != "" {
		q = q.Where("type = ?", typ)
	}
	if st := strings.TrimSpace(filter.Status); st != "" {
		q = q.Where("status = ?", st)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []domain.Feedback
	if err := q.
		Preload("User").
		Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// GetByID loads one feedback row by primary key.
func (r *GormFeedbackRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Feedback, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("feedback id required")
	}
	var row domain.Feedback
	tx := db.WithContext(ctx).Preload("User").Where("id = ?", id).First(&row)
	if tx.Error != nil {
		if tx.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// UpdateStatus sets the triage status for one feedback row.
func (r *GormFeedbackRepository) UpdateStatus(
	ctx context.Context,
	id uuid.UUID,
	status string,
) (*domain.Feedback, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("feedback id required")
	}
	res := db.WithContext(ctx).
		Model(&domain.Feedback{}).
		Where("id = ?", id).
		Update("status", status)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.GetByID(ctx, id)
}
