package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserDataRepository wipes personal skincare data for one account.
type UserDataRepository struct {
	db *gorm.DB
}

// NewUserDataRepository returns a user-data repository.
func NewUserDataRepository(db *gorm.DB) *UserDataRepository {
	return &UserDataRepository{db: db}
}

func (r *UserDataRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// DeleteAllPersonalData soft-deletes every user-owned row (GORM DeletedAt).
// The account row is kept; only diary/profile/shelf data is removed.
// When uploadDir is non-empty, the user's upload subdirectory is removed.
func (r *UserDataRepository) DeleteAllPersonalData(
	ctx context.Context,
	userID uuid.UUID,
	uploadDir string,
) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user id required")
	}

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		subq := tx.Model(&domain.SkinCheck{}).
			Select("id").
			Where("user_id = ?", userID)

		if err := tx.Where("skin_check_id IN (?)", subq).
			Delete(&domain.SkinAnalysis{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.SkinCheck{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.SkinProfile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.RoutineEntry{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.SkincareProduct{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.AIUserFeedback{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.AffiliateClick{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.ProgressLog{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	dir := strings.TrimSpace(uploadDir)
	if dir == "" {
		return nil
	}
	userDir := filepath.Join(filepath.Clean(dir), userID.String())
	_ = os.RemoveAll(userDir)
	return nil
}
