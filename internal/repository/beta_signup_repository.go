package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"gorm.io/gorm"
)

// GormBetaSignupRepository persists public Beta waitlist signups.
type GormBetaSignupRepository struct {
	db *gorm.DB
}

// NewBetaSignupRepository constructs the repository.
func NewBetaSignupRepository(db *gorm.DB) *GormBetaSignupRepository {
	return &GormBetaSignupRepository{db: db}
}

func (r *GormBetaSignupRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// GetByEmail loads one signup by normalized email, or nil when not found.
func (r *GormBetaSignupRepository) GetByEmail(ctx context.Context, email string) (*domain.BetaSignup, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	normalized := strings.TrimSpace(strings.ToLower(email))
	if normalized == "" {
		return nil, fmt.Errorf("email required")
	}
	var row domain.BetaSignup
	tx := db.WithContext(ctx).Where("LOWER(email) = ?", normalized).First(&row)
	if tx.Error != nil {
		if tx.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// Create inserts a Beta waitlist signup row.
func (r *GormBetaSignupRepository) Create(ctx context.Context, row *domain.BetaSignup) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(row).Error
}
