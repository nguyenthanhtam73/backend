package ai

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestMemoryCache_HitAndMiss verifies the basic get/put/expire contract.
func TestMemoryCache_HitAndMiss(t *testing.T) {
	c := NewMemoryCacheWithSize(2*time.Second, 16)
	uid := uuid.New()

	if _, ok := c.Get(uid); ok {
		t.Fatalf("cold cache should miss")
	}

	c.Put(uid, "hello")
	got, ok := c.Get(uid)
	if !ok || got != "hello" {
		t.Fatalf("fresh entry should hit; got=(%q, %v)", got, ok)
	}

	// Wait for the TTL to elapse and confirm the entry is treated as a miss.
	time.Sleep(2100 * time.Millisecond)
	if _, ok := c.Get(uid); ok {
		t.Fatalf("expired entry should miss")
	}
}

// TestMemoryCache_Bust confirms that an explicit Bust removes the entry even
// before the TTL expires (the contract write paths rely on).
func TestMemoryCache_Bust(t *testing.T) {
	c := NewMemoryCacheWithSize(10*time.Minute, 16)
	uid := uuid.New()

	c.Put(uid, "hello")
	c.Bust(uid)
	if _, ok := c.Get(uid); ok {
		t.Fatalf("busted entry should miss")
	}
}

// TestMemoryCache_NilSafe documents that all methods are safe on a nil
// receiver. Several call sites pass through *MemoryCache (which can be nil
// when caching is disabled) and rely on this behaviour.
func TestMemoryCache_NilSafe(t *testing.T) {
	var c *MemoryCache
	uid := uuid.New()

	if _, ok := c.Get(uid); ok {
		t.Fatalf("nil cache Get should miss")
	}
	c.Put(uid, "x") // must not panic
	c.Bust(uid)    // must not panic
	if s := c.Stats(); s.Entries != 0 {
		t.Fatalf("nil cache Stats should be zero, got %+v", s)
	}
}

// TestMemoryCache_Eviction makes sure we don't grow unbounded. We force the
// cap low, fill past it, and confirm the size stays at the cap.
func TestMemoryCache_Eviction(t *testing.T) {
	const cap = 16
	c := NewMemoryCacheWithSize(time.Hour, cap)

	for i := 0; i < cap*2; i++ {
		c.Put(uuid.New(), "x")
	}
	if got := c.Stats().Entries; got > cap {
		t.Fatalf("cache should not exceed cap=%d, got %d", cap, got)
	}
}

// TestBuildUserMemoryContext_EmptyDeps verifies that with no repos wired we
// still return a non-empty, well-formed block (sentinel line).
func TestBuildUserMemoryContext_EmptyDeps(t *testing.T) {
	out := BuildUserMemoryContext(nil, uuid.New(), UserMemoryDeps{}, UserMemoryOptions{})
	if !strings.Contains(out, "USER_MEMORY") {
		t.Fatalf("expected header, got %q", out)
	}
	if !strings.Contains(out, "no saved memory yet") {
		t.Fatalf("expected fresh-user sentinel, got %q", out)
	}
}
