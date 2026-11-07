// Package airecovery replays skin-check analyses and onboarding starter
// routines that were saved while the model providers were out of credit.
//
// A failed photo check-in is a skin_checks row created at or after
// domain.AIAnalysisOutageSince with at least one stored image and a
// skin_analyses row that is not status=completed (failed, missing, or a
// pending/processing row older than the in-flight grace). The failed row
// keeps status=failed and a plain-language error_message; coach JSON stays
// empty and analyzed_at stays null. Completed rows are skipped.
//
// A missing starter routine is a skin_profiles.onboarding_snapshot with
// completed_via=onboarding_v1 that is still the local scaffold (fixed
// encouragement, or starter_routine_pending). User-edited steps and snapshots
// already marked starter_routine_source=ai are skipped.
package airecovery

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

// InFlightGrace matches the analysis job budget closely enough that a replay
// does not start a second run while the first is still working.
const InFlightGrace = 5 * time.Minute

// CheckSnapshot is the slice of a skin check the selector needs.
type CheckSnapshot struct {
	ID                uuid.UUID
	CreatedAt         time.Time
	ImageURLs         json.RawMessage
	HasAnalysis       bool
	Status            domain.AnalysisStatus
	AnalysisUpdatedAt time.Time
}

// CheckNeedsReplay reports whether this photo check-in has no successful
// analysis and is not an in-flight job. since is inclusive.
func CheckNeedsReplay(c CheckSnapshot, since, now time.Time) bool {
	if c.CreatedAt.Before(since) {
		return false
	}
	if !HasStoredPhoto(c.ImageURLs) {
		return false
	}
	if !c.HasAnalysis {
		return true
	}
	switch c.Status {
	case domain.AnalysisStatusCompleted:
		return false
	case domain.AnalysisStatusPending, domain.AnalysisStatusProcessing:
		ref := c.AnalysisUpdatedAt
		if ref.IsZero() {
			ref = c.CreatedAt
		}
		if now.Sub(ref) < InFlightGrace {
			return false
		}
		return true
	default:
		return true
	}
}

// HasStoredPhoto reports whether image_urls contains a non-empty storage key.
func HasStoredPhoto(raw json.RawMessage) bool {
	urls, err := dto.DecodeStringSlice(raw)
	if err != nil || len(urls) == 0 {
		return false
	}
	for _, u := range urls {
		if strings.TrimSpace(u) != "" {
			return true
		}
	}
	return false
}
