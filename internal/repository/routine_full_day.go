package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const routineFullDayIndex = "idx_routine_entries_full_day_live"

// LatestFullRoutineDate returns the newest routine_date whose every stored
// step is ticked, or (nil, nil) when the user has no such day.
//
// One indexed lookup. all_steps_done is maintained on save and backfilled
// once. Postgres uses the partial index idx_routine_entries_full_day_live
// (user_id, routine_date DESC) WHERE all_steps_done AND deleted_at IS NULL.
// Other engines use idx_routine_entries_user_full_day
// (user_id, all_steps_done, routine_date). Checklist JSON is not scanned.
func (r *GormRoutineEntryRepository) LatestFullRoutineDate(ctx context.Context, userID uuid.UUID) (*time.Time, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	var row domain.RoutineEntry
	tx := db.WithContext(ctx).
		Select("routine_date").
		Where("user_id = ? AND all_steps_done = ?", userID, true).
		Order("routine_date DESC").
		Limit(1).
		Take(&row)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	if row.RoutineDate.IsZero() {
		return nil, nil
	}
	day := normalizeDay(row.RoutineDate)
	return &day, nil
}

// applyRoutineFullDay backfills all_steps_done from the checklist JSON, then
// creates the partial index the mascot lookup uses. The index marks the
// backfill as finished, so later boots do not scan routine_entries again.
// SQLite tests set the flag in BeforeSave and use the composite index from
// AutoMigrate, so they skip this path.
func applyRoutineFullDay(db *gorm.DB) error {
	if db == nil || db.Dialector.Name() != "postgres" {
		return nil
	}
	var n int64
	if err := db.Raw(
		`SELECT COUNT(*) FROM pg_indexes WHERE indexname = ?`,
		routineFullDayIndex,
	).Scan(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	marked, err := backfillRoutineAllStepsDone(db)
	if err != nil {
		return err
	}
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_routine_entries_full_day_live
		    ON routine_entries (user_id, routine_date DESC)
		    WHERE all_steps_done AND deleted_at IS NULL
	`).Error; err != nil {
		return err
	}
	slog.Info("routine_entries: full-day index ready", "marked", marked)
	return nil
}

func backfillRoutineAllStepsDone(db *gorm.DB) (int, error) {
	const batch = 200
	var after uuid.UUID
	marked := 0
	for {
		var rows []domain.RoutineEntry
		q := db.Select("id", "morning", "evening").
			Where("all_steps_done = ?", false).
			Order("id").
			Limit(batch)
		if after != uuid.Nil {
			q = q.Where("id > ?", after)
		}
		if err := q.Find(&rows).Error; err != nil {
			return marked, err
		}
		if len(rows) == 0 {
			return marked, nil
		}
		ids := make([]uuid.UUID, 0, len(rows))
		for i := range rows {
			if domain.RoutineAllStepsDone(rows[i].Morning, rows[i].Evening) {
				ids = append(ids, rows[i].ID)
			}
		}
		if len(ids) > 0 {
			if err := db.Model(&domain.RoutineEntry{}).
				Where("id IN ?", ids).
				UpdateColumn("all_steps_done", true).Error; err != nil {
				return marked, err
			}
			marked += len(ids)
		}
		after = rows[len(rows)-1].ID
		if len(rows) < batch {
			return marked, nil
		}
	}
}
