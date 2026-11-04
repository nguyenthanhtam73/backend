package roomcare

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeDays struct {
	calls int
	err   error
	days  map[uuid.UUID][]time.Time
}

func (f *fakeDays) FullRoutineDays(context.Context, []uuid.UUID, time.Time) (map[uuid.UUID][]time.Time, error) {
	f.calls++
	if f.err != nil {
		err := f.err
		f.err = nil
		return nil, err
	}
	return f.days, nil
}

func TestCacheOncePerCivilDay(t *testing.T) {
	today := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	user := uuid.New()
	src := &fakeDays{days: map[uuid.UUID][]time.Time{
		user: {today, today.AddDate(0, 0, -1)},
	}}
	cache := NewCache(src)
	cache.now = func() time.Time { return today }

	first := cache.ForUsers(context.Background(), []uuid.UUID{user, user})
	if first[user] != 2 || src.calls != 1 {
		t.Fatalf("first %#v calls %d", first, src.calls)
	}
	second := cache.ForUsers(context.Background(), []uuid.UUID{user})
	if second[user] != 2 || src.calls != 1 {
		t.Fatalf("cached %#v calls %d", second, src.calls)
	}

	cache.now = func() time.Time { return today.AddDate(0, 0, 1) }
	src.days[user] = []time.Time{today.AddDate(0, 0, 1), today, today.AddDate(0, 0, -1)}
	next := cache.ForUsers(context.Background(), []uuid.UUID{user})
	if next[user] != 3 || src.calls != 2 {
		t.Fatalf("next day %#v calls %d", next, src.calls)
	}
}

func TestRefreshReplacesTheCachedStreak(t *testing.T) {
	today := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	user := uuid.New()
	src := &fakeDays{days: map[uuid.UUID][]time.Time{}}
	cache := NewCache(src)
	cache.now = func() time.Time { return today }
	if got := cache.ForUsers(context.Background(), []uuid.UUID{user}); got[user] != 0 || src.calls != 1 {
		t.Fatalf("empty %#v calls %d", got, src.calls)
	}
	src.days[user] = []time.Time{today, today.AddDate(0, 0, -1)}
	if got := cache.ForUsers(context.Background(), []uuid.UUID{user}); got[user] != 0 || src.calls != 1 {
		t.Fatalf("stale %#v calls %d", got, src.calls)
	}
	if got := cache.Refresh(context.Background(), []uuid.UUID{user}); got[user] != 2 || src.calls != 2 {
		t.Fatalf("refresh %#v calls %d", got, src.calls)
	}
}

func TestCacheDoesNotStoreSourceError(t *testing.T) {
	today := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	user := uuid.New()
	src := &fakeDays{
		err:  context.DeadlineExceeded,
		days: map[uuid.UUID][]time.Time{user: {today}},
	}
	cache := NewCache(src)
	cache.now = func() time.Time { return today }
	if got := cache.ForUsers(context.Background(), []uuid.UUID{user}); got[user] != 0 || src.calls != 1 {
		t.Fatalf("error %#v calls %d", got, src.calls)
	}
	if got := cache.ForUsers(context.Background(), []uuid.UUID{user}); got[user] != 1 || src.calls != 2 {
		t.Fatalf("retry %#v calls %d", got, src.calls)
	}
}
