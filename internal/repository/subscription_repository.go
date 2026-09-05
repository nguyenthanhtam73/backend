package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SubscriptionRepository persists append-only subscription history rows.
type SubscriptionRepository struct {
	db *gorm.DB
}

// NewSubscriptionRepository returns a subscriptions history repository.
func NewSubscriptionRepository(db *gorm.DB) *SubscriptionRepository {
	return &SubscriptionRepository{db: db}
}

func (r *SubscriptionRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts one history row.
func (r *SubscriptionRepository) Create(ctx context.Context, row *domain.Subscription) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("subscription row required")
	}
	return db.WithContext(ctx).Create(row).Error
}

// CreateTx inserts within an existing transaction.
func (r *SubscriptionRepository) CreateTx(tx *gorm.DB, row *domain.Subscription) error {
	if tx == nil {
		return fmt.Errorf("transaction required")
	}
	if row == nil {
		return fmt.Errorf("subscription row required")
	}
	return tx.Create(row).Error
}

// ListForUser returns newest-first history for a user.
func (r *SubscriptionRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
	limit int,
) ([]domain.Subscription, error) {
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
	var rows []domain.Subscription
	err = db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// ExistsByExternalRef reports whether a history row already used this provider ref
// (idempotency helper for SePay renewals).
func (r *SubscriptionRepository) ExistsByExternalRef(ctx context.Context, externalRef string) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	externalRef = strings.TrimSpace(externalRef)
	if externalRef == "" {
		return false, nil
	}
	var count int64
	err = db.WithContext(ctx).
		Model(&domain.Subscription{}).
		Where("external_ref = ?", externalRef).
		Count(&count).Error
	return count > 0, err
}

// ListOpenOverdue returns non-terminal subscription rows whose billed period
// has already ended. Used by billing reconcile (cron + operator command).
func (r *SubscriptionRepository) ListOpenOverdue(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]domain.Subscription, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	var rows []domain.Subscription
	err = db.WithContext(ctx).
		Where("status IN ?", domain.OpenSubscriptionStatuses()).
		Where("period_ends_at IS NOT NULL AND period_ends_at <= ?", now.UTC()).
		Order("period_ends_at ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// ListOpenByUserTx returns non-terminal rows for one user (inside a transaction).
func (r *SubscriptionRepository) ListOpenByUserTx(tx *gorm.DB, userID uuid.UUID) ([]domain.Subscription, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction required")
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	var rows []domain.Subscription
	err := tx.Where("user_id = ? AND status IN ?", userID, domain.OpenSubscriptionStatuses()).
		Order("created_at DESC").
		Find(&rows).Error
	return rows, err
}

// MarkOverdueOpenTx sets status on open rows whose period_ends_at has passed.
func (r *SubscriptionRepository) MarkOverdueOpenTx(
	tx *gorm.DB,
	userID uuid.UUID,
	now time.Time,
	to domain.SubscriptionStatus,
) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("transaction required")
	}
	if userID == uuid.Nil {
		return 0, fmt.Errorf("user id required")
	}
	to = domain.NormalizeSubscriptionStatus(to)
	res := tx.Model(&domain.Subscription{}).
		Where("user_id = ?", userID).
		Where("status IN ?", domain.OpenSubscriptionStatuses()).
		Where("status <> ?", to).
		Where("period_ends_at IS NOT NULL AND period_ends_at <= ?", now.UTC()).
		Updates(map[string]any{
			"status":     to,
			"updated_at": now.UTC(),
		})
	return res.RowsAffected, res.Error
}

// CloseOpenTx sets status on every open row for the user (renewal / trial replace).
func (r *SubscriptionRepository) CloseOpenTx(
	tx *gorm.DB,
	userID uuid.UUID,
	to domain.SubscriptionStatus,
	now time.Time,
) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("transaction required")
	}
	if userID == uuid.Nil {
		return 0, fmt.Errorf("user id required")
	}
	to = domain.NormalizeSubscriptionStatus(to)
	res := tx.Model(&domain.Subscription{}).
		Where("user_id = ? AND status IN ?", userID, domain.OpenSubscriptionStatuses()).
		Updates(map[string]any{
			"status":     to,
			"updated_at": now.UTC(),
		})
	return res.RowsAffected, res.Error
}

// MarkOpenStatusesTx sets status on rows matching fromStatuses for one user.
func (r *SubscriptionRepository) MarkOpenStatusesTx(
	tx *gorm.DB,
	userID uuid.UUID,
	from []domain.SubscriptionStatus,
	to domain.SubscriptionStatus,
	now time.Time,
) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("transaction required")
	}
	if userID == uuid.Nil {
		return 0, fmt.Errorf("user id required")
	}
	if len(from) == 0 {
		return 0, nil
	}
	to = domain.NormalizeSubscriptionStatus(to)
	res := tx.Model(&domain.Subscription{}).
		Where("user_id = ? AND status IN ?", userID, from).
		Updates(map[string]any{
			"status":     to,
			"updated_at": now.UTC(),
		})
	return res.RowsAffected, res.Error
}

// ListCoveringUserIDs returns distinct user_ids that have a history row whose
// period_ends_at is still inside the billed window or grace at `now`.
func (r *SubscriptionRepository) ListCoveringUserIDs(
	ctx context.Context,
	now time.Time,
	graceDays int,
	limit int,
) ([]uuid.UUID, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	graceDays = domain.ClampGraceDays(graceDays)
	cutoff := now.UTC().Add(-domain.DaysDuration(graceDays))
	var ids []uuid.UUID
	err = db.WithContext(ctx).
		Model(&domain.Subscription{}).
		Where("period_ends_at IS NOT NULL AND period_ends_at > ?", cutoff).
		Distinct("user_id").
		Limit(limit).
		Pluck("user_id", &ids).Error
	return ids, err
}

// FindBestCoveringForUserTx returns the history row with the latest period_ends_at
// that still covers now (period or grace). Nil when none exists.
func (r *SubscriptionRepository) FindBestCoveringForUserTx(
	tx *gorm.DB,
	userID uuid.UUID,
	now time.Time,
	graceDays int,
) (*domain.Subscription, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction required")
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	graceDays = domain.ClampGraceDays(graceDays)
	cutoff := now.UTC().Add(-domain.DaysDuration(graceDays))
	var row domain.Subscription
	err := tx.Where("user_id = ? AND period_ends_at IS NOT NULL AND period_ends_at > ?", userID, cutoff).
		Order("period_ends_at DESC").
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// MarkCoveringActiveTx re-opens a history row that still has a future
// period_ends_at but was left expired/past_due (repair, not a new period).
func (r *SubscriptionRepository) MarkCoveringActiveTx(
	tx *gorm.DB,
	id uuid.UUID,
	now time.Time,
) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("transaction required")
	}
	if id == uuid.Nil {
		return 0, fmt.Errorf("subscription id required")
	}
	res := tx.Model(&domain.Subscription{}).
		Where("id = ?", id).
		Where("period_ends_at IS NOT NULL AND period_ends_at > ?", now.UTC()).
		Where("canceled_at IS NULL").
		Where("status IN ?", []domain.SubscriptionStatus{
			domain.SubStatusExpired, domain.SubStatusPastDue,
		}).
		Updates(map[string]any{
			"status":     domain.SubStatusActive,
			"updated_at": now.UTC(),
		})
	return res.RowsAffected, res.Error
}

// CountActiveInPeriod counts subscription rows that are currently billed
// (status=active and period_ends_at still in the future). Lifetime grants
// (NULL period_ends_at) are excluded — they live on users.plan_expires_at.
func (r *SubscriptionRepository) CountActiveInPeriod(ctx context.Context, now time.Time) (int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, err
	}
	var n int64
	err = db.WithContext(ctx).
		Model(&domain.Subscription{}).
		Where("status = ?", domain.SubStatusActive).
		Where("period_ends_at IS NOT NULL AND period_ends_at > ?", now.UTC()).
		Count(&n).Error
	return n, err
}

// HasEventType reports whether a history row of eventType exists for the user.
func (r *SubscriptionRepository) HasEventTypeTx(tx *gorm.DB, userID uuid.UUID, eventType domain.SubscriptionEventType) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("transaction required")
	}
	if userID == uuid.Nil {
		return false, fmt.Errorf("user id required")
	}
	var n int64
	err := tx.Model(&domain.Subscription{}).
		Where("user_id = ? AND event_type = ?", userID, eventType).
		Count(&n).Error
	return n > 0, err
}
