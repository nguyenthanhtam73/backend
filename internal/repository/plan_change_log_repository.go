package repository

import (
	"context"
	"fmt"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PlanChangeLogRepository persists admin plan grant/revoke audits.
type PlanChangeLogRepository struct {
	db *gorm.DB
}

// NewPlanChangeLogRepository returns a plan-change log repository.
func NewPlanChangeLogRepository(db *gorm.DB) *PlanChangeLogRepository {
	return &PlanChangeLogRepository{db: db}
}

func (r *PlanChangeLogRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts one audit row.
func (r *PlanChangeLogRepository) Create(ctx context.Context, row *domain.PlanChangeLog) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("plan change log required")
	}
	return db.WithContext(ctx).Create(row).Error
}

// CreateTx inserts within an existing transaction.
func (r *PlanChangeLogRepository) CreateTx(tx *gorm.DB, row *domain.PlanChangeLog) error {
	if tx == nil {
		return fmt.Errorf("transaction required")
	}
	if row == nil {
		return fmt.Errorf("plan change log required")
	}
	return tx.Create(row).Error
}

// ListForUser returns newest-first audit rows for a target user.
func (r *PlanChangeLogRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
	limit int,
) ([]domain.PlanChangeLog, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var rows []domain.PlanChangeLog
	err = db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}
