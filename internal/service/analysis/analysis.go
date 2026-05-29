// Package analysis runs the skin-check AI pipeline and persists SkinAnalysis rows.
// skincheck.Service calls Process synchronously before HTTP returns so clients get coach JSON immediately.
// It loads SkinProfile (when repo is wired) so Claude/GPT personalize feedback with long-term skin type, undertone, and onboarding goals.
// Preferred path: GPT vision (observations only) → Anthropic Claude (structured coach JSON).
// Fallback: single GPT multimodal call when DADIARY_ANTHROPIC_API_KEY is not set.
package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/google/uuid"
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
	cache *ai.MemoryCache,
) *Service {
	return &Service{
		cfg:      cfg,
		checks:   checks,
		profiles: profiles,
		feedback: feedback,
		routines: routines,
		cache:    cache,
		httpClient: &http.Client{
			Timeout: 8 * time.Minute,
		},
	}
}

// EnqueueAnalysis starts a background job.
func (s *Service) EnqueueAnalysis(skinCheckID uuid.UUID) {
	if s == nil || skinCheckID == uuid.Nil {
		return
	}
	go func(id uuid.UUID) {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		_ = s.Process(ctx, id)
	}(skinCheckID)
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

	// USER_MEMORY — long-term context (profile + last few check-ins with prior
	// AI feedback + thumbs votes + routine adherence). Built lazily by the
	// shared helper so future surfaces (weekly review, reminders) reuse the
	// same wording. Excludes the current SkinCheck so the model doesn't see
	// "today" twice.
	//
	// We deliberately omit `Cache` here: ExcludeCheckID is set, which
	// disables caching anyway (each excluded-row variant has different
	// output and shouldn't share a cache entry).
	//
	// We log the debug summary at Info level so prompt-loop diagnostics are
	// trivially greppable (`grep "user_memory injected"` in stderr). The
	// builder itself also slog.Debug's the same thing — Info here is a
	// per-call breadcrumb tied to a specific SkinCheck.
	memory, memDebug := ai.BuildUserMemoryWithDebug(
		ctx,
		o.UserID,
		ai.UserMemoryDeps{
			Profiles: s.profiles,
			Checks:   s.checks,
			Feedback: s.feedback,
			Routines: s.routines,
		},
		ai.UserMemoryOptions{ExcludeCheckID: o.ID},
	)
	ai.LogMemoryInjection("skin-check", o.UserID, o.ID, memDebug)

	parsed, ver, err := ai.RunSkinCheckCoach(ctx, s.cfg, s.httpClient, uploadRoot, o, prof, memory)
	if err != nil {
		return s.failWithMessage(ctx, skinCheckID, err.Error())
	}

	a2, err := s.checks.GetByID(ctx, skinCheckID)
	if err != nil || a2 == nil || a2.Analysis == nil {
		return err
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
	up.AvoidOrPatch = av
	up.SafetyFlags = sf
	up.SummaryNotes = strings.TrimSpace(parsed.SummaryNotes)

	modern := time.Now().UTC()
	up.AnalyzedAt = &modern
	up.ErrorMessage = ""

	if err := s.checks.SaveAnalysis(ctx, up); err != nil {
		return err
	}
	// Fresh analysis row → next AI call (Routine Suggest, Daily Feedback,
	// or /me/memory) should see it. Busting now keeps the cache honest
	// without waiting for the TTL.
	s.cache.Bust(o.UserID)
	return nil
}

func (s *Service) failWithMessage(ctx context.Context, skinCheckID uuid.UUID, msg string) error {
	o, err := s.checks.GetByID(ctx, skinCheckID)
	if err != nil {
		return err
	}
	if o == nil || o.Analysis == nil {
		return fmt.Errorf("%s", msg)
	}
	a := o.Analysis
	a.Status = domain.AnalysisStatusFailed
	a.ErrorMessage = msg
	return s.checks.SaveAnalysis(ctx, a)
}
