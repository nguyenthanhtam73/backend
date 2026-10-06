package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/storage"
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
// The account row is kept; only diary/profile/shelf data is removed. Stored
// photo files are removed separately by the use case via the storage backend.
func (r *UserDataRepository) DeleteAllPersonalData(
	ctx context.Context,
	userID uuid.UUID,
) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user id required")
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deletePersonalRows(tx, userID, false)
	})
}

// deletePersonalRows removes diary rows for userID.
// hard is false for DELETE /me/data (GORM soft-delete where the model has DeletedAt;
// the account row stays). hard is true for account deletion so foreign keys and
// previously soft-deleted rows cannot keep the person linked.
func deletePersonalRows(tx *gorm.DB, userID uuid.UUID, hard bool) error {
	scope := func(q *gorm.DB) *gorm.DB {
		if hard {
			return q.Unscoped()
		}
		return q
	}
	checkQ := scope(tx).Model(&domain.SkinCheck{}).Select("id").Where("user_id = ?", userID)
	if err := scope(tx).Where("skin_check_id IN (?)", checkQ).Delete(&domain.SkinAnalysis{}).Error; err != nil {
		return err
	}
	models := []any{
		&domain.SkinCheck{},
		&domain.SkinProfile{},
		&domain.RoutineEntry{},
		&domain.SkincareProduct{},
		&domain.AIUserFeedback{},
		&domain.Feedback{},
		&domain.AffiliateClick{},
		&domain.ProgressLog{},
		&domain.PushSubscription{},
		&domain.PushSendReceipt{},
		&domain.CheckInReminderFlag{},
		&domain.RoutineSuggestJob{},
		// Streak / usage rows have no DeletedAt — Delete removes them either way.
		&domain.Streak{},
		&domain.UserUsage{},
	}
	for _, model := range models {
		if err := scope(tx).Where("user_id = ?", userID).Delete(model).Error; err != nil {
			return err
		}
	}
	return nil
}

// ListPhotoKeys returns every stored photo key owned by the user (check-ins,
// onboarding, progress logs) so GDPR wipe can delete date-prefixed R2 objects
// that DeletePrefix(userID+"/") would miss.
func (r *UserDataRepository) ListPhotoKeys(ctx context.Context, userID uuid.UUID) ([]string, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}

	seen := map[string]struct{}{}
	add := func(raw json.RawMessage) {
		rels, _ := dto.DecodeStringSlice(raw)
		for _, rel := range rels {
			k := storage.CleanKey(rel)
			if k == "" {
				continue
			}
			seen[k] = struct{}{}
		}
	}

	var checks []domain.SkinCheck
	if err := db.WithContext(ctx).
		Select("image_urls").
		Where("user_id = ?", userID).
		Find(&checks).Error; err != nil {
		return nil, err
	}
	for i := range checks {
		add(checks[i].ImageURLs)
	}

	var profiles []domain.SkinProfile
	if err := db.WithContext(ctx).
		Select("photo_urls").
		Where("user_id = ?", userID).
		Find(&profiles).Error; err != nil {
		return nil, err
	}
	for i := range profiles {
		add(profiles[i].PhotoURLs)
	}

	var logs []domain.ProgressLog
	if err := db.WithContext(ctx).
		Select("image_urls").
		Where("user_id = ?", userID).
		Find(&logs).Error; err != nil {
		return nil, err
	}
	for i := range logs {
		add(logs[i].ImageURLs)
	}

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out, nil
}
