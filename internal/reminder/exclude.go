package reminder

import (
	"context"
	"fmt"
	"regexp"
	"sync"

	"github.com/dadiary/backend/internal/domain"
	"gorm.io/gorm"
)

// userIDExprPattern keeps the column name out of SQL injection. Callers pass
// a fixed identifier such as "user_id".
var userIDExprPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

type observeKey struct{}

type observedSet struct {
	mu  sync.Mutex
	ids map[JobID]struct{}
}

// ObserveContext records which jobs call ExcludeMuted. Production contexts
// are a no-op. The mute test uses this so a job that forgets its own JobID
// fails even when another job's filter still drops the same rows.
func ObserveContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, observeKey{}, &observedSet{ids: map[JobID]struct{}{}})
}

// Observed reports whether ExcludeMuted was called with id on ctx.
func Observed(ctx context.Context, id JobID) bool {
	set := observedFrom(ctx)
	if set == nil {
		return false
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	_, ok := set.ids[id]
	return ok
}

func observedFrom(ctx context.Context) *observedSet {
	if ctx == nil {
		return nil
	}
	set, _ := ctx.Value(observeKey{}).(*observedSet)
	return set
}

func noteObserved(ctx context.Context, jobs []JobID) {
	set := observedFrom(ctx)
	if set == nil {
		return
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	for _, id := range jobs {
		set.ids[id] = struct{}{}
	}
}

// ExcludeMuted drops users with reminder_enabled = false from a candidate
// query. NULL and true stay in the result. jobs must be registered in All;
// an empty list or an unknown id panics so a new reminder cannot ship
// without joining the registry.
//
// userIDExpr is the outer query's user id column, for example "user_id".
func ExcludeMuted(ctx context.Context, db *gorm.DB, userIDExpr string, jobs ...JobID) *gorm.DB {
	if len(jobs) == 0 {
		panic("reminder.ExcludeMuted: pass the reminder job id")
	}
	for _, id := range jobs {
		if !registered(id) {
			panic(fmt.Sprintf("reminder.ExcludeMuted: %q is not in reminder.All", id))
		}
	}
	if !userIDExprPattern.MatchString(userIDExpr) {
		panic("reminder.ExcludeMuted: user id column must be a plain SQL identifier")
	}
	noteObserved(ctx, jobs)
	muted := db.Session(&gorm.Session{NewDB: true}).
		Model(&domain.User{}).
		Select("id").
		Where("reminder_enabled = ?", false)
	return db.Where(userIDExpr+" NOT IN (?)", muted)
}

// ExcludeScheduled drops users who saved a capture schedule
// (reminder_enabled true and a non-empty reminder_time) from a fixed-clock
// candidate query. NULL and true-without-time stay in the result. OFF users
// are not this function's job — callers still use ExcludeMuted.
//
// The scheduled_capture job is the only sender for a saved schedule, so the
// 20:00 pushes, the 19:30 email, and the hourly D0/D1 passes cannot take a
// second moment the same day.
func ExcludeScheduled(db *gorm.DB, userIDExpr string) *gorm.DB {
	if db == nil {
		return db
	}
	if !userIDExprPattern.MatchString(userIDExpr) {
		panic("reminder.ExcludeScheduled: user id column must be a plain SQL identifier")
	}
	scheduled := db.Session(&gorm.Session{NewDB: true}).
		Model(&domain.User{}).
		Select("id").
		Where("reminder_enabled = ?", true).
		Where("reminder_time IS NOT NULL AND reminder_time <> ?", "")
	return db.Where(userIDExpr+" NOT IN (?)", scheduled)
}
