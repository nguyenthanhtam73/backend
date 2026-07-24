// Package skincheck implements daily skin check-in use cases.
package skincheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/analysis"
	"github.com/dadiary/backend/internal/service/moderation"
	"github.com/dadiary/backend/internal/storage"
	"github.com/dadiary/backend/internal/streaktime"
	streakuc "github.com/dadiary/backend/internal/usecase/streak"
	"github.com/google/uuid"
)

var (
	ErrInvalidInput       = errors.New("invalid skin check payload")
	ErrModerationRejected = errors.New("content did not pass moderation")
	ErrDatabase           = errors.New("database error")
)

// UploadImage is one decoded, validated photo handed from the HTTP layer. Bytes
// are kept in memory so moderation can run on them and the storage backend
// (local disk or R2) can persist them after content passes checks.
type UploadImage struct {
	Rel         string // relative storage key, e.g. "<userID>/<uuid>.jpg"
	Data        []byte
	ContentType string
}

// CreateInput is built by the HTTP layer after files are decoded and validated.
type CreateInput struct {
	Title           string
	UserNote        string
	Conditions      []string
	Symptoms        []string
	ClimateContext  json.RawMessage // optional JSON object from client (weather snapshot, etc.)
	EnvironmentNote string
	Visibility      domain.CheckVisibility
	Images          []UploadImage
	// SkipMode allows tag/notes-only check-ins with zero images (privacy path).
	SkipMode bool
}

// Service orchestrates skin checks and AI analysis jobs.
type Service struct {
	cfg      *config.Config
	checks   *repository.GormSkinCheckRepository
	mod      *moderation.Service
	analyzer *analysis.Service
	store    storage.Storage
	streaks  StreakRecorder
	tx       repository.TxRunner
}

// StreakRecorder updates the user's check-in streak after a SkinCheck is saved.
// Prefer calling inside the same DB transaction as CreateWithAnalysis (via TxRunner).
//
// Auto-freeze may be applied inside RecordCheckIn when the user missed exactly
// one day — the returned result tells the client so it can show a distinct toast.
type StreakRecorder interface {
	RecordCheckIn(ctx context.Context, userID uuid.UUID, checkDate time.Time) (streakuc.CheckInResult, error)
}

// NewService wires dependencies. streaks and tx may be nil (streak then skipped).
func NewService(
	cfg *config.Config,
	checks *repository.GormSkinCheckRepository,
	mod *moderation.Service,
	analyzer *analysis.Service,
	store storage.Storage,
	streaks StreakRecorder,
	tx repository.TxRunner,
) *Service {
	return &Service{
		cfg:      cfg,
		checks:   checks,
		mod:      mod,
		analyzer: analyzer,
		store:    store,
		streaks:  streaks,
		tx:       tx,
	}
}

// Create runs moderation, persists SkinCheck + pending SkinAnalysis, enqueues AI in the background, and returns immediately so the client can poll GET /skin-checks/:id.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in CreateInput) (dto.CreateSkinCheckResponse, error) {
	var zero dto.CreateSkinCheckResponse
	if s == nil || s.checks == nil {
		return zero, fmt.Errorf("%w: persistence unavailable", ErrDatabase)
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("%w: user id required", ErrInvalidInput)
	}
	hasTextSignal := strings.TrimSpace(in.UserNote) != "" ||
		strings.TrimSpace(in.EnvironmentNote) != "" ||
		len(in.Conditions) > 0 ||
		len(in.Symptoms) > 0
	if len(in.Images) == 0 {
		if !in.SkipMode {
			return zero, fmt.Errorf("%w: at least one image required", ErrInvalidInput)
		}
		if !hasTextSignal {
			return zero, fmt.Errorf("%w: skip-mode check-in needs tags or a note", ErrInvalidInput)
		}
	}
	// Photo uploads need storage; skip-mode (no images) does not.
	if len(in.Images) > 0 && s.store == nil {
		return zero, fmt.Errorf("%w: storage unavailable", ErrDatabase)
	}

	vis := in.Visibility
	if vis == "" {
		vis = domain.CheckVisibilityPrivate
	}
	if vis != domain.CheckVisibilityPrivate && vis != domain.CheckVisibilityPublic {
		return zero, fmt.Errorf("%w: invalid visibility", ErrInvalidInput)
	}

	if s.mod != nil {
		text := strings.TrimSpace(in.Title + "\n" + in.UserNote + "\n" + in.EnvironmentNote)
		if len(in.Conditions) > 0 {
			text += "\n" + strings.Join(in.Conditions, " ")
		}
		if len(in.Symptoms) > 0 {
			text += "\n" + strings.Join(in.Symptoms, " ")
		}
		imgBytes := make([][]byte, 0, len(in.Images))
		for _, img := range in.Images {
			imgBytes = append(imgBytes, img.Data)
		}
		modStart := time.Now()
		modErr := s.mod.CheckSkinContent(ctx, text, imgBytes)
		slog.Info("skin-check: moderation done",
			"user_id", userID,
			"images", len(imgBytes),
			"duration_ms", time.Since(modStart).Milliseconds(),
			"rejected", modErr != nil,
		)
		if err := modErr; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "moderation") ||
				strings.Contains(strings.ToLower(err.Error()), "flagged") {
				return zero, fmt.Errorf("%w: %v", ErrModerationRejected, err)
			}
			return zero, err
		}
	}

	// Persist bytes only after moderation passes, so rejected content never lands
	// in storage. The relative key is what we store in the DB.
	relPaths := make([]string, 0, len(in.Images))
	for _, img := range in.Images {
		if err := s.store.Save(ctx, img.Rel, img.Data, img.ContentType); err != nil {
			return zero, fmt.Errorf("%w: save image: %v", ErrDatabase, err)
		}
		relPaths = append(relPaths, img.Rel)
	}

	imgJSON, err := json.Marshal(relPaths)
	if err != nil {
		return zero, err
	}
	var condJSON json.RawMessage
	if len(in.Conditions) > 0 {
		condJSON, err = json.Marshal(in.Conditions)
		if err != nil {
			return zero, err
		}
	}
	var symJSON json.RawMessage
	if len(in.Symptoms) > 0 {
		symJSON, err = json.Marshal(in.Symptoms)
		if err != nil {
			return zero, err
		}
	}
	var climateJSON json.RawMessage
	if len(in.ClimateContext) > 0 {
		climateJSON = append(json.RawMessage(nil), in.ClimateContext...)
	}

	checkD := streaktime.DateOf(time.Now())

	check := &domain.SkinCheck{
		UserID:          userID,
		Title:           strings.TrimSpace(in.Title),
		UserNote:        strings.TrimSpace(in.UserNote),
		ImageURLs:       imgJSON,
		Conditions:      condJSON,
		Symptoms:        symJSON,
		ClimateContext:  climateJSON,
		EnvironmentNote: strings.TrimSpace(in.EnvironmentNote),
		Visibility:      vis,
		CheckDate:       checkD,
	}

	analysisRow := &domain.SkinAnalysis{
		Status: domain.AnalysisStatusPending,
	}

	// Persist SkinCheck + pending Analysis + Streak in ONE transaction when a
	// TxRunner is wired. That way a streak write failure rolls back the diary
	// row too — no "check exists, streak skipped" drift. Images are already on
	// disk (pre-tx); orphan files on rollback are acceptable vs inconsistent streak.
	var streakMeta *dto.SkinCheckStreakMeta
	persist := func(txCtx context.Context) error {
		if err := s.checks.CreateWithAnalysis(txCtx, check, analysisRow); err != nil {
			return err
		}
		if s.streaks != nil {
			result, err := s.streaks.RecordCheckIn(txCtx, userID, check.CheckDate)
			if err != nil {
				return fmt.Errorf("streak: %w", err)
			}
			streakMeta = &dto.SkinCheckStreakMeta{
				AutoFreezeApplied:   result.AutoFreezeApplied,
				CatchUpContinued:    result.CatchUpContinued,
				UnusedFreezeCleared: result.UnusedFreezeCleared,
				CurrentStreak:       result.Streak.CurrentStreak,
				FreezesAvailable:    result.Streak.FreezesAvailable,
				ProtectedUntil:      result.Streak.ProtectedUntil,
			}
		}
		return nil
	}

	var persistErr error
	if s.tx != nil {
		persistErr = s.tx.WithinTransaction(ctx, persist)
	} else {
		// Fallback when TxRunner is not configured (tests / partial wiring):
		// keep prior sequential behaviour without a shared transaction.
		persistErr = persist(ctx)
	}
	if persistErr != nil {
		return zero, fmt.Errorf("%w: %v", ErrDatabase, persistErr)
	}

	publicURLs := make([]string, 0, len(relPaths))
	for _, rel := range relPaths {
		clean := storage.CleanKey(rel)
		publicURLs = append(publicURLs, "/"+path.Join("uploads", clean))
	}

	if s.analyzer != nil {
		s.analyzer.EnqueueAnalysis(check.ID)
	} else {
		slog.Warn("skin-check: analysis service not configured", "check_id", check.ID)
	}

	reloadCtx, reloadCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer reloadCancel()
	reloaded, err := s.checks.GetByID(reloadCtx, check.ID)
	if err != nil || reloaded == nil {
		return zero, fmt.Errorf("%w: reload skin check after analysis", ErrDatabase)
	}

	res := dto.NewCreateSkinCheckResponse(reloaded, reloaded.Analysis, publicURLs)
	res.Streak = streakMeta
	return res, nil
}
