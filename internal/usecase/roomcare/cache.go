package roomcare

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

// DaySource lists full-routine civil days for a set of accounts.
// since is inclusive. The result omits partial days.
type DaySource interface {
	FullRoutineDays(ctx context.Context, userIDs []uuid.UUID, since time.Time) (map[uuid.UUID][]time.Time, error)
}

// Cache stores one care streak per account per Vietnam civil day.
// ForUsers computes on a miss and then reuses the int. Refresh recomputes,
// which is what a room GET and the diligence board do. Presence frames call
// ForUsers, so a tick does not query.
type Cache struct {
	days DaySource
	now  func() time.Time

	mu     sync.Mutex
	day    string
	values map[uuid.UUID]int
}

// NewCache builds a day cache. now defaults to the Vietnam civil today.
func NewCache(days DaySource) *Cache {
	return &Cache{days: days, values: map[uuid.UUID]int{}}
}

// ForUsers returns care streaks for ids. A cache hit for this Vietnam day
// does not touch the database. A miss is one FullRoutineDays query for the
// uncached ids. A source error is not cached.
func (c *Cache) ForUsers(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]int {
	return c.load(ctx, ids, false)
}

// Refresh recomputes ids and replaces the cached ints for this Vietnam day.
// Room GET and the diligence board use it. Presence frames keep using ForUsers.
func (c *Cache) Refresh(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]int {
	return c.load(ctx, ids, true)
}

func (c *Cache) load(ctx context.Context, ids []uuid.UUID, refresh bool) map[uuid.UUID]int {
	out := map[uuid.UUID]int{}
	if c == nil {
		return out
	}
	today := c.today()
	day := today.Format("2006-01-02")
	var miss []uuid.UUID
	seen := map[uuid.UUID]struct{}{}
	c.mu.Lock()
	if c.day != day {
		c.day = day
		c.values = map[uuid.UUID]int{}
	}
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if n, ok := c.values[id]; ok && !refresh {
			out[id] = n
			continue
		}
		miss = append(miss, id)
	}
	c.mu.Unlock()
	if len(miss) == 0 || c.days == nil {
		return out
	}
	found, err := c.days.FullRoutineDays(ctx, miss, Lookback(today))
	if err != nil {
		slog.Error("room care streak", "error", err)
		for _, id := range miss {
			out[id] = 0
		}
		return out
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	store := c.day == day
	for _, id := range miss {
		n := CareStreak(today, found[id])
		out[id] = n
		if store {
			c.values[id] = n
		}
	}
	return out
}

func (c *Cache) today() time.Time {
	if c != nil && c.now != nil {
		return streaktime.DateOf(c.now())
	}
	return streaktime.Today()
}
