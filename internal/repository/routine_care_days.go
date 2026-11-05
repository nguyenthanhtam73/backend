package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

// FullRoutineDays returns, for each user, the Vietnam civil days in
// [since, today] on which at least one session is fully done. A session needs
// one real step and every one of those steps ticked. A day whose morning and
// evening are both still open is omitted. One query for the whole id list,
// using the routine_entries (user_id, routine_date) index.
func (r *GormRoutineEntryRepository) FullRoutineDays(ctx context.Context, userIDs []uuid.UUID, since time.Time) (map[uuid.UUID][]time.Time, error) {
	out := map[uuid.UUID][]time.Time{}
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	ids := uniqueIDs(userIDs)
	if len(ids) == 0 {
		return out, nil
	}
	since = streaktime.DateOf(since)
	var rows []struct {
		UserID      uuid.UUID
		RoutineDate time.Time
		Morning     json.RawMessage
		Evening     json.RawMessage
	}
	err = db.WithContext(ctx).
		Model(&domain.RoutineEntry{}).
		Select("user_id", "routine_date", "morning", "evening").
		Where("user_id IN ? AND routine_date >= ?", ids, since).
		Order("routine_date DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]map[string]struct{}{}
	for _, row := range rows {
		if !domain.RoutineSessionDone(row.Morning, row.Evening) {
			continue
		}
		day := streaktime.DateOf(row.RoutineDate)
		key := day.Format("2006-01-02")
		if seen[row.UserID] == nil {
			seen[row.UserID] = map[string]struct{}{}
		}
		if _, dup := seen[row.UserID][key]; dup {
			continue
		}
		seen[row.UserID][key] = struct{}{}
		out[row.UserID] = append(out[row.UserID], day)
	}
	return out, nil
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
