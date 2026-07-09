// Package analysis runs the skin-check AI pipeline and persists SkinAnalysis rows.
// skincheck.Service enqueues Process in the background; clients poll GET /skin-checks/:id for results.
// It loads SkinProfile (when repo is wired) so Claude/GPT personalize feedback with long-term skin type, undertone, and onboarding goals.
// Preferred path: GPT vision (observations only) → Anthropic Claude (structured coach JSON).
// Fallback: single GPT multimodal call when DADIARY_ANTHROPIC_API_KEY is not set.
package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/google/uuid"
)

const (
	// analysisJobTimeout caps total wall time for one skin-check AI job (vision + coach).
	analysisJobTimeout = 4 * time.Minute
	// analysisHTTPTimeout caps each outbound LLM HTTP call so one slow provider cannot block for many minutes.
	analysisHTTPTimeout = 2 * time.Minute
	// staleAnalysisAge marks pending/processing rows as failed when polled after the job should have finished.
	staleAnalysisAge = analysisJobTimeout + 30*time.Second
	staleFailedMessage = "Analysis timed out. Please try submitting again."
)

// Service runs the skin-check AI pipeline (optionally enriched with SkinProfile for personalization).
//
// `feedback` and `routines` are optional — when present, the pipeline injects
// the user's long-term memory (profile, prior check-ins with AI feedback,
// thumbs-up/down votes, routine adherence) into the prompt so the coach can
// adapt tone, avoid repeating angles the user marked 👎, and calibrate
// suggestion difficulty against actual adherence.
//
// `cache` is the shared in-process memory cache. When non-nil it is busted
// after every new SkinAnalysis completes so the next AI call (or /me/memory
// fetch) sees the freshly-analysed check immediately instead of waiting out
// the TTL.
type Service struct {
	httpClient *http.Client
	cfg        *config.Config
	checks     *repository.GormSkinCheckRepository
	profiles   *repository.GormSkinProfileRepository
	feedback   *repository.GormAIFeedbackRepository
	routines   *repository.GormRoutineEntryRepository
	wardrobe   *repository.GormSkincareProductRepository
	cache      *ai.MemoryCache
}

// New constructs the analysis worker. profiles, feedback, routines, and
// cache may each be nil (the coach degrades gracefully — uses check-in only,
// and no caching when cache is nil).
func New(
	cfg *config.Config,
	checks *repository.GormSkinCheckRepository,
	profiles *repository.GormSkinProfileRepository,
	feedback *repository.GormAIFeedbackRepository,
	routines *repository.GormRoutineEntryRepository,
	wardrobe *repository.GormSkincareProductRepository,
	cache *ai.MemoryCache,
) *Service {
	return &Service{
		cfg:      cfg,
		checks:   checks,
		profiles: profiles,
		feedback: feedback,
		routines: routines,
		wardrobe: wardrobe,
		cache:    cache,
		httpClient: &http.Client{
			Timeout: analysisHTTPTimeout,
		},
	}
}

// EnqueueAnalysis starts a background job.
func (s *Service) EnqueueAnalysis(skinCheckID uuid.UUID) {
	if s == nil || skinCheckID == uuid.Nil {
		return
	}
	go func(id uuid.UUID) {
		ctx, cancel := context.WithTimeout(context.Background(), analysisJobTimeout)
		defer cancel()
		if err := s.Process(ctx, id); err != nil {
			slog.Warn("skin-check: background analysis failed", "check_id", id, "err", err)
		}
	}(skinCheckID)
}

// ExpireStaleAnalysis marks long-running pending/processing rows as failed so clients stop polling forever.
// Returns true when the row was updated.
func ExpireStaleAnalysis(ctx context.Context, checks *repository.GormSkinCheckRepository, a *domain.SkinAnalysis) bool {
	if checks == nil || a == nil {
		return false
	}
	if a.Status != domain.AnalysisStatusPending && a.Status != domain.AnalysisStatusProcessing {
		return false
	}
	ref := a.UpdatedAt
	if ref.IsZero() {
		ref = a.CreatedAt
	}
	if time.Since(ref) < staleAnalysisAge {
		return false
	}
	a.Status = domain.AnalysisStatusFailed
	a.ErrorMessage = staleFailedMessage
	if err := checks.SaveAnalysis(ctx, a); err != nil {
		return false
	}
	return true
}

// Process loads images, runs the AI coach pipeline, and saves structured fields to SkinAnalysis.
func (s *Service) Process(ctx context.Context, skinCheckID uuid.UUID) error {
	if s == nil || s.checks == nil || s.cfg == nil {
		return fmt.Errorf("analysis: not configured")
	}
	if strings.TrimSpace(s.cfg.OpenAI.APIKey) == "" {
		return s.failWithMessage(ctx, skinCheckID, "OpenAI API key missing (required for photo analysis vision pass)")
	}

	o, err := s.checks.GetByID(ctx, skinCheckID)
	if err != nil {
		return err
	}
	if o == nil || o.Analysis == nil {
		return fmt.Errorf("skin check or analysis not found")
	}

	a := o.Analysis
	a.Status = domain.AnalysisStatusProcessing
	// Placeholder until pipeline returns the real version string (includes Claude + vision models).
	a.ModelVersion = "pending"
	if err := s.checks.SaveAnalysis(ctx, a); err != nil {
		return err
	}

	uploadRoot := s.cfg.Upload.Dir
	var prof *domain.SkinProfile
	if s.profiles != nil {
		p, perr := s.profiles.GetByUserID(ctx, o.UserID)
		if perr == nil {
			prof = p
		}
	}

	// USER_MEMORY and vision pass are independent — run them in parallel so
	// wall time is max(memory DB, vision API) instead of their sum.
	urls, urlErr := dto.DecodeStringSlice(o.ImageURLs)
	if urlErr != nil || len(urls) == 0 {
		return fmt.Errorf("analysis: no image paths")
	}

	var (
		memory   string
		memDebug ai.MemoryDebug
		visionRaw string
		visionStatus string
	)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		memory, memDebug = ai.BuildUserMemoryWithDebug(
			ctx,
			o.UserID,
			ai.UserMemoryDeps{
				Profiles: s.profiles,
				Checks:   s.checks,
				Feedback: s.feedback,
				Routines: s.routines,
				Wardrobe: s.wardrobe,
			},
			ai.UserMemoryOptions{ExcludeCheckID: o.ID},
		)
	}()
	go func() {
		defer wg.Done()
		visionRaw, visionStatus = ai.RunVisionObservationPassForCheck(
			ctx, s.cfg, s.httpClient, uploadRoot, urls,
		)
	}()
	wg.Wait()
	ai.LogMemoryInjection("skin-check", o.UserID, o.ID, memDebug)

	parsed, ver, err := ai.RunSkinCheckCoachAfterVision(
		ctx, s.cfg, s.httpClient, o, prof, memory, visionRaw, visionStatus,
	)
	if err != nil {
		return s.failWithMessage(ctx, skinCheckID, err.Error())
	}

	a2, err := s.checks.GetByID(ctx, skinCheckID)
	if err != nil || a2 == nil || a2.Analysis == nil {
		return s.failWithMessage(ctx, skinCheckID, "could not reload analysis after coach run")
	}
	up := a2.Analysis

	labels := parsed.SkinScores
	if labels == nil {
		labels = map[string]any{}
	}
	labels["overall"] = parsed.Score
	labels["concern_alignment"] = parsed.ConcernAlignment
	if strings.TrimSpace(parsed.SituationAnalysis) != "" {
		labels["situation_analysis"] = strings.TrimSpace(parsed.SituationAnalysis)
	}
	ss, _ := json.Marshal(labels)

	str, _ := json.Marshal(parsed.Strengths)
	imp, _ := json.Marshal(parsed.Improvements)
	rh, _ := json.Marshal(parsed.RoutineHints)
	ps, _ := json.Marshal(parsed.ProductSuggestions)
	av, _ := json.Marshal(parsed.AvoidOrPatch)

	disclaimer := strings.TrimSpace(parsed.MedicalDisclaimer)
	if disclaimer == "" {
		disclaimer = ai.DefaultMedicalDisclaimerVI
	}
	reminders := parsed.SafetyReminders
	if reminders == nil {
		reminders = []string{}
	}
	safetyObj := map[string]any{
		"reminders":  reminders,
		"disclaimer": disclaimer,
	}
	sf, _ := json.Marshal(safetyObj)

	up.Status = domain.AnalysisStatusCompleted
	up.ModelVersion = ver
	up.PromptVersion = ai.CoachDailyPromptVersion
	up.SkinScores = ss
	up.Strengths = str
	up.Improvements = imp
	up.RoutineHints = rh
	up.ProductSuggestions = ps
	up.AvoidOrPatch = av
	up.SafetyFlags = sf
	up.SummaryNotes = strings.TrimSpace(parsed.SummaryNotes)

	modern := time.Now().UTC()
	up.AnalyzedAt = &modern
	up.ErrorMessage = ""

	// Use a fresh context for the final persist — the job ctx may be nearly
	// exhausted after vision + coach + validation retries.
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer saveCancel()
	if err := s.checks.SaveAnalysis(saveCtx, up); err != nil {
		slog.Warn("skin-check: save completed analysis failed",
			"check_id", skinCheckID,
			"model_version_len", len(up.ModelVersion),
			"err", err,
		)
		return s.failWithMessage(ctx, skinCheckID, "could not save completed analysis")
	}
	// Fresh analysis row → next AI call (Routine Suggest, Daily Feedback,
	// or /me/memory) should see it. Busting now keeps the cache honest
	// without waiting for the TTL.
	if s.cache != nil {
		s.cache.Bust(o.UserID)
	}
	return nil
}

func (s *Service) failWithMessage(_ context.Context, skinCheckID uuid.UUID, msg string) error {
	saveCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	o, err := s.checks.GetByID(saveCtx, skinCheckID)
	if err != nil {
		return err
	}
	if o == nil || o.Analysis == nil {
		return fmt.Errorf("%s", msg)
	}
	a := o.Analysis
	a.Status = domain.AnalysisStatusFailed
	a.ErrorMessage = msg
	if err := s.checks.SaveAnalysis(saveCtx, a); err != nil {
		return fmt.Errorf("%s (save failed: %v)", msg, err)
	}
	return fmt.Errorf("%s", msg)
}
