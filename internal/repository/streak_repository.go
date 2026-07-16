package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormStreakRepository persists per-user streak rows.
type GormStreakRepository struct {
	db *gorm.DB
}

// NewStreakRepository constructs the repository.
func NewStreakRepository(db *gorm.DB) *GormStreakRepository {
	return &GormStreakRepository{db: db}
}

func (r *GormStreakRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// ListUsersAtRisk returns user IDs whose streak is "at risk" for today's
// Vietnam civil calendar — aligned with dto.EvaluateStreakView IsAtRisk:
//
//  1. days_since == 1 (last_check_in = yesterday), not protected through today
//  2. days_since == 2 + ProtectedUntil == yesterday (freeze bridged the miss)
//  3. days_since == 2 + FreezesAvailable > 0 (pending auto-freeze on check-in)
//
// Soft-expired gap-2 rows (no freeze left / no bridge) are excluded — they are
// not IsAtRisk in EvaluateStreakView. Daily Reminder skips this same set so
// at-risk users only get streak_at_risk.
func (r *GormStreakRepository) ListUsersAtRisk(ctx context.Context) ([]uuid.UUID, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	today := streaktime.Today()
	yesterday := today.AddDate(0, 0, -1)
	dayBeforeYesterday := today.AddDate(0, 0, -2)

	conn := DBFromContext(ctx, db)
	var ids []uuid.UUID
	err = conn.
		Model(&domain.Streak{}).
		Where("current_streak > ?", 0).
		Where(
			`(last_check_in_date = ? AND (protected_until IS NULL OR protected_until < ?))
			 OR (last_check_in_date = ? AND protected_until = ?)
			 OR (last_check_in_date = ? AND freezes_available > 0)`,
			yesterday, today,
			dayBeforeYesterday, yesterday,
			dayBeforeYesterday,
		).
		Pluck("user_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// GetByUserID returns the streak row, or (nil, nil) when missing.
func (r *GormStreakRepository) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Streak, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	conn := DBFromContext(ctx, db)
	var row domain.Streak
	err = conn.Where("user_id = ?", userID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UpdateAtomic loads (or creates) the user's streak under SELECT FOR UPDATE,
// runs mutate, then saves. mutate must not commit/rollback the transaction.
//
// If ctx already carries a transaction (repository.WithTx), this joins it so
// SkinCheck + Streak can commit/rollback as one unit. Otherwise a dedicated
// transaction is opened.
func (r *GormStreakRepository) UpdateAtomic(
	ctx context.Context,
	userID uuid.UUID,
	mutate func(row *domain.Streak) error,
) (*domain.Streak, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if mutate == nil {
		return nil, fmt.Errorf("mutate is required")
	}

	var out *domain.Streak
	run := func(tx *gorm.DB) error {
		row, err := lockOrCreateStreak(tx, userID)
		if err != nil {
			return err
		}
		if err := mutate(row); err != nil {
			return err
		}
		if err := tx.Save(row).Error; err != nil {
			return err
		}
		out = row
		return nil
	}

	if existing := TxFromContext(ctx); existing != nil {
		if err := run(existing); err != nil {
			return nil, err
		}
		return out, nil
	}

	txErr := db.WithContext(ctx).Transaction(run)
	if txErr != nil {
		return nil, txErr
	}
	return out, nil
}

func lockOrCreateStreak(tx *gorm.DB, userID uuid.UUID) (*domain.Streak, error) {
	var row domain.Streak
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = domain.Streak{
			UserID:           userID,
			CurrentStreak:    0,
			LongestStreak:    0,
			FreezesAvailable: domain.DefaultFreezesAvailable,
		}
		if createErr := tx.Create(&row).Error; createErr != nil {
			// Concurrent first-create: another writer won the insert — lock theirs.
			if lockErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ?", userID).
				First(&row).Error; lockErr != nil {
				return nil, createErr
			}
			return &row, nil
		}
		if lockErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userID).
			First(&row).Error; lockErr != nil {
			return nil, lockErr
		}
		return &row, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
