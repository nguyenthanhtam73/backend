// Package usermemory exposes the read-only "what does the AI know about me?"
// endpoint. The handler wraps a single call to ai.BuildUserMemoryContext and
// attaches a small diagnostic block (char count, cache stats, history counts)
// so debugging the prompt loop doesn't require a database client.
//
// All write paths live elsewhere — this package is intentionally minimal.
package usermemory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/google/uuid"
)

// ErrUnavailable is returned when the service is not fully wired (e.g. DB
// down at startup). Handlers translate this into a 503.
var ErrUnavailable = errors.New("memory service unavailable")

// Service backs GET /api/v1/me/memory.
type Service struct {
	checks   *repository.GormSkinCheckRepository
	profiles *repository.GormSkinProfileRepository
	feedback *repository.GormAIFeedbackRepository
	routines *repository.GormRoutineEntryRepository
	wardrobe *repository.GormSkincareProductRepository
	cache    *ai.MemoryCache
}

// NewService wires dependencies. Every repo + cache argument is optional;
func NewService(
	checks *repository.GormSkinCheckRepository,
	profiles *repository.GormSkinProfileRepository,
	feedback *repository.GormAIFeedbackRepository,
	routines *repository.GormRoutineEntryRepository,
	wardrobe *repository.GormSkincareProductRepository,
	cache *ai.MemoryCache,
) *Service {
	return &Service{
		checks:   checks,
		profiles: profiles,
		feedback: feedback,
		routines: routines,
		wardrobe: wardrobe,
		cache:    cache,
	}
}

// Get builds the memory block for `userID` and returns the public response.
//
// When `forceFresh` is true, we bypass the cache entirely — useful when the
// frontend "refresh" button is pressed or when an admin wants ground truth.
// Otherwise this respects the cache and labels the response accordingly.
func (s *Service) Get(ctx context.Context, userID uuid.UUID, forceFresh bool) (dto.UserMemoryResponse, error) {
	var zero dto.UserMemoryResponse
	if s == nil {
		return zero, ErrUnavailable
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("%w: user id required", ErrUnavailable)
	}

	memText, debug := ai.BuildUserMemoryWithDebug(
		ctx,
		userID,
		ai.UserMemoryDeps{
			Profiles: s.profiles,
			Checks:   s.checks,
			Feedback: s.feedback,
			Routines: s.routines,
			Wardrobe: s.wardrobe,
			Cache:    s.cache,
		},
		ai.UserMemoryOptions{SkipCache: forceFresh},
	)

	// Per-section counters come straight from MemoryDebug — no double
	// query. TotalChecks may already be populated when the monthly-digest
	// section fired; otherwise we fall back to one extra Count call so
	// the response always has the right number.
	stats := dto.UserMemoryStats{
		CharCount:        debug.CharCount,
		SectionsPresent:  debug.SectionsPresent,
		HelpfulVotes:     debug.HelpfulVotes,
		NotHelpfulVotes:  debug.NotHelpfulVotes,
		AdherenceTier:    debug.AdherenceTier,
		HasMonthlyDigest: debug.HasMonthlyDigest,
		TotalChecks:      debug.TotalChecks,
		PromptVersion:    ai.CoachDailyPromptVersion,
	}
	if stats.TotalChecks == 0 && s.checks != nil {
		if n, err := s.checks.CountForUser(ctx, userID); err == nil {
			stats.TotalChecks = n
		}
	}
	// TotalFeedback is the full-history count (not just the 12-row window
	// the builder samples). Bound the query at 200 — anyone with more
	// than that doesn't need a precise count.
	if s.feedback != nil {
		if rows, err := s.feedback.ListByUser(ctx, userID, 200); err == nil {
			stats.TotalFeedback = len(rows)
		}
	}
	if s.cache != nil {
		cs := s.cache.Stats()
		stats.CacheEntries = cs.Entries
		stats.CacheTTLSeconds = cs.TTLSeconds
	}

	return dto.UserMemoryResponse{
		UserID:      userID.String(),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Cached:      debug.CacheHit,
		MemoryText:  memText,
		Stats:       stats,
	}, nil
}
