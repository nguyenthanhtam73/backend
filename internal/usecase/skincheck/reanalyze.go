package skincheck

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/service/analysis"
	"github.com/dadiary/backend/internal/storage"
	"github.com/google/uuid"
)

// Reanalyze re-runs vision on the stored photos for an owned skin check.
//
// It does not upload new images, change check_date, or touch streak. The
// existing SkinAnalysis row is marked processing and the shared
// Process/EnqueueAnalysis pipeline replaces it when the job finishes. Clients
// poll GET /skin-checks/:id.
//
// Idempotent while a job is in flight (pending/processing): returns the current
// payload and does not enqueue a second job. Completed checks may be re-run
// once per UTC day; failed analyses may retry the same day.
//
// Voice: the background job reads skin_checks.client_kind, not the live request.
// clientKind is the current X-DaDiary-Client value. An exact "android" upgrades
// the stored kind to android before enqueue (and before the in-flight early
// return, so a job that has not loaded the row yet can still pick up the polite
// voice). A web reanalyze never downgrades an android check — the Play app must
// not later show a crude rewrite of a check it owns. A web-created check stays
// on today's voice until an Android reanalyze upgrades it.
func (s *Service) Reanalyze(ctx context.Context, userID, checkID uuid.UUID, clientKind string) (dto.CreateSkinCheckResponse, error) {
	var zero dto.CreateSkinCheckResponse
	if s == nil || s.checks == nil {
		return zero, fmt.Errorf("%w: persistence unavailable", ErrDatabase)
	}
	if userID == uuid.Nil || checkID == uuid.Nil {
		return zero, fmt.Errorf("%w: user id and check id required", ErrInvalidInput)
	}

	check, err := s.checks.GetByIDForOwner(ctx, checkID, userID)
	if err != nil {
		return zero, fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	if check == nil {
		return zero, ErrNotFound
	}

	if check.Analysis != nil {
		analysis.ExpireStaleAnalysis(ctx, s.checks, check.Analysis)
	}

	if !hasStoredPhotos(check.ImageURLs) {
		return zero, ErrNoPhotos
	}
	if check.Analysis == nil {
		return zero, fmt.Errorf("%w: analysis row missing", ErrDatabase)
	}

	switch check.Analysis.Status {
	case domain.AnalysisStatusPending, domain.AnalysisStatusProcessing:
		// The job may not have loaded the row yet. Upgrade first so it can
		// still pick up the polite voice. Do not enqueue a second run.
		if err := s.upgradeAndroidVoice(ctx, check, clientKind); err != nil {
			return zero, fmt.Errorf("%w: %v", ErrDatabase, err)
		}
		return s.skinCheckResponse(check), nil
	}

	if reanalyzeLimitReached(check.Analysis, time.Now().UTC()) {
		return zero, ErrReanalyzeLimit
	}

	if s.analyzer == nil {
		slog.Warn("skin-check: reanalyze skipped — analysis service not configured", "check_id", check.ID)
		return zero, fmt.Errorf("%w: analysis service not configured", ErrDatabase)
	}

	if err := s.upgradeAndroidVoice(ctx, check, clientKind); err != nil {
		return zero, fmt.Errorf("%w: %v", ErrDatabase, err)
	}

	claimed, err := s.checks.ClaimReanalyze(ctx, check.ID, time.Now().UTC())
	if err != nil {
		return zero, fmt.Errorf("%w: claim reanalyze: %v", ErrDatabase, err)
	}
	if !claimed {
		reloaded, rerr := s.checks.GetByIDForOwner(ctx, checkID, userID)
		if rerr != nil {
			return zero, fmt.Errorf("%w: %v", ErrDatabase, rerr)
		}
		if reloaded == nil {
			return zero, ErrNotFound
		}
		if reloaded.Analysis != nil {
			switch reloaded.Analysis.Status {
			case domain.AnalysisStatusPending, domain.AnalysisStatusProcessing:
				return s.skinCheckResponse(reloaded), nil
			}
			if reanalyzeLimitReached(reloaded.Analysis, time.Now().UTC()) {
				return zero, ErrReanalyzeLimit
			}
		}
		return zero, fmt.Errorf("%w: could not claim reanalyze", ErrDatabase)
	}

	s.analyzer.EnqueueAnalysis(check.ID)

	reloaded, err := s.checks.GetByIDForOwner(ctx, checkID, userID)
	if err != nil || reloaded == nil {
		return zero, fmt.Errorf("%w: reload skin check after reanalyze claim", ErrDatabase)
	}
	return s.skinCheckResponse(reloaded), nil
}

// upgradeAndroidVoice persists client_kind=android when this request is from
// the Play app. A web request leaves the stored kind alone, so an Android
// check is never rewritten in the crude voice.
func (s *Service) upgradeAndroidVoice(ctx context.Context, check *domain.SkinCheck, clientKind string) error {
	if check == nil || domain.NormalizeRefreshClient(clientKind) != domain.RefreshClientAndroid {
		return nil
	}
	if domain.NormalizeRefreshClient(check.ClientKind) == domain.RefreshClientAndroid {
		check.ClientKind = domain.RefreshClientAndroid
		return nil
	}
	if err := s.checks.SetClientKind(ctx, check.ID, domain.RefreshClientAndroid); err != nil {
		return err
	}
	check.ClientKind = domain.RefreshClientAndroid
	return nil
}

func reanalyzeLimitReached(a *domain.SkinAnalysis, now time.Time) bool {
	if a == nil || a.Status != domain.AnalysisStatusCompleted {
		return false
	}
	if a.LastReanalyzedAt == nil {
		return false
	}
	now = now.UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return !a.LastReanalyzedAt.UTC().Before(dayStart)
}

func hasStoredPhotos(raw []byte) bool {
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

func (s *Service) skinCheckResponse(check *domain.SkinCheck) dto.CreateSkinCheckResponse {
	if check == nil {
		return dto.CreateSkinCheckResponse{}
	}
	rels, _ := dto.DecodeStringSlice(check.ImageURLs)
	publicURLs := make([]string, 0, len(rels))
	for _, rel := range rels {
		clean := storage.CleanKey(rel)
		if clean == "" {
			continue
		}
		publicURLs = append(publicURLs, "/"+path.Join("uploads", clean))
	}
	return dto.NewCreateSkinCheckResponse(check, check.Analysis, publicURLs)
}
