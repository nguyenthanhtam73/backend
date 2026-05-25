// Package ai — user_memory_cache.go is a tiny TTL+LRU-ish in-memory cache for
// the BuildUserMemoryContext block.
//
// Why this exists:
//   - BuildUserMemoryContext costs 4 SQL queries (profile, recent checks +
//     analysis, feedback, routines) on every AI call. The Daily Skin-check
//     pipeline and Routine Suggest can be hit many times per user per day;
//     under contention this becomes a measurable share of overall API time.
//   - The data this block summarises only changes on three discrete events:
//     a new skin check, a new AI feedback vote, or a routine upsert. Outside
//     those events the cached value is identical for minutes-to-hours. A
//     short TTL (default 5 minutes) is therefore "free": it captures the
//     normal request bursts while staying stale-tolerant for everything else.
//
// Design choices:
//   - In-process only. We deliberately do NOT pull in Redis for Stage 2 — a
//     single API node carries this load comfortably, and a process-local
//     cache keeps the deploy story simple. A future Redis adapter can
//     implement the same Get/Put/Bust contract.
//   - Cap on entries (`maxEntries`) prevents unbounded memory growth from
//     long-tail user IDs. When the cap is hit we evict expired entries
//     first, then drop the oldest "expiresAt" entry. Not a true LRU — but
//     for 5-minute TTL across <10k users it is more than adequate.
//   - Thread-safe via sync.RWMutex (reads dominate writes).
package ai

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultMemoryCacheTTL is the default lifetime of a cached memory block.
	// Five minutes is the sweet spot: long enough to absorb back-to-back AI
	// calls during a single user session, short enough that explicit user
	// actions (new check-in, new vote) are reflected promptly even without
	// an explicit Bust.
	DefaultMemoryCacheTTL = 5 * time.Minute

	// DefaultMemoryCacheMaxEntries caps in-process memory at roughly
	// max_entries * ~3KB ≈ 30MB worst case. Plenty for a single-node deploy;
	// configurable via NewMemoryCacheWithSize when needed.
	DefaultMemoryCacheMaxEntries = 10000
)

// MemoryCache stores BuildUserMemoryContext output per user with a TTL.
//
// The zero value is NOT ready to use — always construct via NewMemoryCache.
// A nil *MemoryCache is safe to pass anywhere — all methods short-circuit
// on nil receiver so callers can wire optional caching without nil-checks.
type MemoryCache struct {
	ttl        time.Duration
	maxEntries int
	mu         sync.RWMutex
	store      map[uuid.UUID]memoryCacheEntry
}

type memoryCacheEntry struct {
	text      string
	expiresAt time.Time
}

// NewMemoryCache returns a cache with the default TTL + size.
func NewMemoryCache() *MemoryCache {
	return NewMemoryCacheWithSize(DefaultMemoryCacheTTL, DefaultMemoryCacheMaxEntries)
}

// NewMemoryCacheWithSize lets callers (mostly tests) tune the TTL + cap.
//
// Both arguments are clamped to safe minimums: TTL is forced to >= 1 second
// (anything shorter defeats the purpose) and maxEntries to >= 16 so we
// always have room for a handful of concurrent sessions.
func NewMemoryCacheWithSize(ttl time.Duration, maxEntries int) *MemoryCache {
	if ttl < time.Second {
		ttl = DefaultMemoryCacheTTL
	}
	if maxEntries < 16 {
		maxEntries = DefaultMemoryCacheMaxEntries
	}
	return &MemoryCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		store:      make(map[uuid.UUID]memoryCacheEntry, 64),
	}
}

// Get returns (text, true) on a fresh hit. Stale entries are treated as
// misses (and lazily removed). On a nil receiver this returns ("", false).
func (c *MemoryCache) Get(userID uuid.UUID) (string, bool) {
	if c == nil || userID == uuid.Nil {
		return "", false
	}
	c.mu.RLock()
	entry, ok := c.store[userID]
	c.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		// Re-check under write lock to avoid racing with a fresh Put.
		if cur, stillThere := c.store[userID]; stillThere && time.Now().After(cur.expiresAt) {
			delete(c.store, userID)
		}
		c.mu.Unlock()
		return "", false
	}
	return entry.text, true
}

// Put stores text under userID with a fresh TTL. Empty text and Nil userID
// are no-ops so callers can pass through whatever they have without guards.
func (c *MemoryCache) Put(userID uuid.UUID, text string) {
	if c == nil || userID == uuid.Nil || text == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.store) >= c.maxEntries {
		c.evictLocked()
	}
	c.store[userID] = memoryCacheEntry{
		text:      text,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// Bust removes the cached entry for one user. Use this from write paths
// (new skin check, new feedback vote, routine upsert) to make sure the
// next AI call reflects the change without waiting out the TTL.
func (c *MemoryCache) Bust(userID uuid.UUID) {
	if c == nil || userID == uuid.Nil {
		return
	}
	c.mu.Lock()
	delete(c.store, userID)
	c.mu.Unlock()
}

// evictLocked clears expired entries first; if nothing is expired, drops the
// entry with the earliest expiry. Caller must hold the write lock.
func (c *MemoryCache) evictLocked() {
	now := time.Now()
	for k, v := range c.store {
		if now.After(v.expiresAt) {
			delete(c.store, k)
		}
	}
	if len(c.store) < c.maxEntries {
		return
	}
	// Fallback: kill the oldest entry. We iterate the whole map — fine at
	// our scale (max ~10k entries) and avoids a full priority-queue.
	var (
		oldestKey uuid.UUID
		oldestAt  = time.Now().Add(24 * time.Hour)
	)
	for k, v := range c.store {
		if v.expiresAt.Before(oldestAt) {
			oldestAt = v.expiresAt
			oldestKey = k
		}
	}
	if oldestKey != uuid.Nil {
		delete(c.store, oldestKey)
	}
}

// Stats returns a small diagnostic snapshot. Used by the /me/memory debug
// endpoint and useful in tests.
type MemoryCacheStats struct {
	Entries    int           `json:"entries"`
	TTLSeconds float64       `json:"ttl_seconds"`
	MaxEntries int           `json:"max_entries"`
	TTL        time.Duration `json:"-"`
}

// Stats reports the current entry count and TTL. Safe to call on nil.
func (c *MemoryCache) Stats() MemoryCacheStats {
	if c == nil {
		return MemoryCacheStats{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return MemoryCacheStats{
		Entries:    len(c.store),
		TTLSeconds: c.ttl.Seconds(),
		MaxEntries: c.maxEntries,
		TTL:        c.ttl,
	}
}
