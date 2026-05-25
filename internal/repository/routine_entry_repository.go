// Package repository — routine_entry_repository.go persists daily skincare
// routines (AM/PM step arrays stored as JSON in routine_entries).
//
// Design rationale:
//   - There is **at most one row per (user, routine_date)**. The unique key is
//     enforced softly by GetByUserAndDate + UpsertForDay in code (Postgres also
//     has the index from AutoMigrate). This makes the daily tick-list a single
//     row to fetch and patch, no JOINs.
//   - History queries (`?range=30`) lean on the existing index on
//     (user_id, routine_date). We deliberately don't preload anything — steps
//     already live in JSONB on the same row.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormRoutineEntryRepository persists RoutineEntry rows.
type GormRoutineEntryRepository struct {
	db *gorm.DB
}

// NewRoutineEntryRepository returns a routine repository.
func NewRoutineEntryRepository(db *gorm.DB) *GormRoutineEntryRepository {
	return &GormRoutineEntryRepository{db: db}
}

func (r *GormRoutineEntryRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// GetByUserAndDate fetches a routine entry for a specific calendar day, or nil.
// `day` is normalized to UTC midnight before the lookup so callers can pass any
// `time.Time` from a request and still hit the row.
func (r *GormRoutineEntryRepository) GetByUserAndDate(ctx context.Context, userID uuid.UUID, day time.Time) (*domain.RoutineEntry, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	d := normalizeDay(day)
	var row domain.RoutineEntry
	tx := db.WithContext(ctx).
		Where("user_id = ? AND routine_date = ?", userID, d).
		First(&row)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// GetLatestForUser returns the most recent routine entry for a user (any date),
// or nil. Used to "carry forward" yesterday's routine into today before the user
// has saved anything for the current day.
func (r *GormRoutineEntryRepository) GetLatestForUser(ctx context.Context, userID uuid.UUID) (*domain.RoutineEntry, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var row domain.RoutineEntry
	tx := db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("routine_date DESC, updated_at DESC").
		First(&row)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// UpsertForDay creates or updates the single routine entry for (user, day).
//
// We intentionally fetch-then-create-or-update instead of using ON CONFLICT
// because not every Postgres deployment has the partial-unique index yet (the
// model uses `gorm:"index"` on routine_date, not unique). This is a low-write
// path (one row per day per user) so the extra read is fine.
func (r *GormRoutineEntryRepository) UpsertForDay(ctx context.Context, entry *domain.RoutineEntry) (*domain.RoutineEntry, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if entry == nil || entry.UserID == uuid.Nil {
		return nil, fmt.Errorf("invalid routine entry")
	}
	entry.RoutineDate = normalizeDay(entry.RoutineDate)

	existing, err := r.GetByUserAndDate(ctx, entry.UserID, entry.RoutineDate)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		if err := db.WithContext(ctx).Create(entry).Error; err != nil {
			return nil, err
		}
		return entry, nil
	}

	existing.Morning = entry.Morning
	existing.Evening = entry.Evening
	existing.Notes = entry.Notes
	if entry.Source != "" {
		existing.Source = entry.Source
	}
	if entry.SkillMode != "" {
		existing.SkillMode = entry.SkillMode
	}
	if err := db.WithContext(ctx).Save(existing).Error; err != nil {
		return nil, err
	}
	return existing, nil
}

// ListForUserSince returns the user's routine entries with routine_date >= since
// (UTC midnight). Most-recent first. Pass time.Time{} for "all history". `limit`
// is a hard cap (defaults to 365 if zero/negative).
func (r *GormRoutineEntryRepository) ListForUserSince(ctx context.Context, userID uuid.UUID, since time.Time, limit int) ([]domain.RoutineEntry, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 365 {
		limit = 365
	}
	q := db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("routine_date DESC").
		Limit(limit)
	if !since.IsZero() {
		q = q.Where("routine_date >= ?", normalizeDay(since))
	}
	var rows []domain.RoutineEntry
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// normalizeDay truncates a time to UTC midnight so all comparisons against the
// `routine_date` DATE column match regardless of the caller's timezone offset.
func normalizeDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
