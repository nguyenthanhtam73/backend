// Package dto — user_memory.go is the response shape for the
// GET /api/v1/me/memory debug/inspect endpoint.
package dto

// UserMemoryResponse is what GET /me/memory returns to the authenticated user.
//
// This endpoint is meant for the frontend "what does the AI know about me?"
// preview and for backend debugging. It exposes the same memory string that
// gets injected into AI coach prompts (so devs can sanity-check the loop) plus
// a few small diagnostic counters and cache stats.
//
// NOTE: We do NOT redact anything here — the data already belongs to the
// authenticated caller. JWT middleware guarantees that.
type UserMemoryResponse struct {
	// UserID echoes the caller — useful when this endpoint is hit from
	// internal tooling on behalf of another user.
	UserID string `json:"user_id"`
	// GeneratedAt is when the block was built (RFC3339, UTC). When `Cached`
	// is true this is the original build time, not the read time.
	GeneratedAt string `json:"generated_at"`
	// Cached indicates whether this response came from the in-process TTL
	// cache (`true`) or was assembled fresh from the DB (`false`).
	Cached bool `json:"cached"`
	// MemoryText is the markdown-style block exactly as injected into
	// AI prompts. Newline-separated; safe to render in a <pre>.
	MemoryText string `json:"memory_text"`
	// Stats is a small diagnostic snapshot — character count, cache size,
	// total checks on file. Helps tune token budgets without scraping
	// every response.
	Stats UserMemoryStats `json:"stats"`
}

// UserMemoryStats is the small diagnostics block attached to the memory
// response. All counts are best-effort — a query failure on one counter
// returns zero for that counter, not an error for the whole endpoint.
type UserMemoryStats struct {
	// CharCount is len([]rune(memory)) — a rough proxy for token count
	// (English: ~4 chars/token, Vietnamese: ~5–6).
	CharCount int `json:"char_count"`
	// TotalChecks is the total skin checks on file for the user. Above
	// the digest threshold the memory block emits a per-month summary.
	TotalChecks int64 `json:"total_checks"`
	// TotalFeedback is the total number of thumbs-up/down votes recorded.
	TotalFeedback int `json:"total_feedback"`
	// CacheEntries is the global in-process cache entry count (NOT
	// per-user). Exposed so an admin can spot a hot cache during incidents.
	CacheEntries int `json:"cache_entries"`
	// CacheTTLSeconds is the cache TTL the server is configured with.
	CacheTTLSeconds float64 `json:"cache_ttl_seconds"`

	// --- Per-section diagnostic from MemoryDebug ---

	// SectionsPresent lists which markdown sections ended up in MemoryText.
	// e.g. ["profile", "recent_checks", "routine_adherence"]. Useful to
	// answer "why didn't the coach reference my votes?" — empty list
	// means that section's repo returned no rows.
	SectionsPresent []string `json:"sections_present"`
	// HelpfulVotes / NotHelpfulVotes are the split of the most recent
	// feedback window (whatever the builder pulled — usually 12 rows).
	HelpfulVotes    int `json:"helpful_votes"`
	NotHelpfulVotes int `json:"not_helpful_votes"`
	// AdherenceTier is one of "strong" | "moderate" | "low" | "none"
	// (empty when no routine data on file). Lets the frontend show a
	// "Routine streak: strong" pill next to the memory preview.
	AdherenceTier string `json:"adherence_tier"`
	// HasMonthlyDigest is true when the user crossed the digest
	// threshold (>50 checks by default) and the older-history monthly
	// digest section was rendered.
	HasMonthlyDigest bool `json:"has_monthly_digest"`
	// PromptVersion echoes ai.CoachDailyPromptVersion — lets the
	// frontend label the memory preview with the prompt iteration that
	// generated the most recent analyses.
	PromptVersion int `json:"prompt_version"`
}
