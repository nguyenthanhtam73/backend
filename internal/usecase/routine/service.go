// Package routine implements the Routine Management use cases:
//
//   - GetCurrent     → read today's routine (or yesterday carried forward)
//   - Upsert         → create/update today's routine
//   - History        → list last N days for the progress view
//   - Suggest        → call the AI service for a fresh AM/PM suggestion
//
// This package is the thin orchestrator between the routine repository, the
// skin profile repository (read-only, for AI context), and the AI suggest
// service. HTTP concerns (auth, JSON encoding) stay in the handler.
package routine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/google/uuid"
)

var (
	ErrInvalidInput = errors.New("invalid routine payload")
	ErrUnavailable  = errors.New("routine service unavailable")
)

// Service is the Routine use-case façade.
type Service struct {
	cfg       *config.Config
	routines  *repository.GormRoutineEntryRepository
	profiles  *repository.GormSkinProfileRepository
	skinCheck *repository.GormSkinCheckRepository
	// feedback is optional — when wired, Suggest reads recent feedback
	// rows so the AI can adapt to the user's tone preferences.
	feedback *repository.GormAIFeedbackRepository
	// cache is the shared in-process memory cache; busted after every
	// Upsert so adherence stats reflect the new tick immediately.
	cache *ai.MemoryCache
}

// NewService wires dependencies. skinCheck, feedback, and cache are optional —
// when nil, the AI suggest call runs with profile-only context (and no cache).
func NewService(
	cfg *config.Config,
	routines *repository.GormRoutineEntryRepository,
	profiles *repository.GormSkinProfileRepository,
	skinCheck *repository.GormSkinCheckRepository,
	feedback *repository.GormAIFeedbackRepository,
	cache *ai.MemoryCache,
) *Service {
	return &Service{
		cfg:       cfg,
		routines:  routines,
		profiles:  profiles,
		skinCheck: skinCheck,
		feedback:  feedback,
		cache:     cache,
	}
}

// GetCurrent returns today's routine if it exists. Otherwise it falls back to
// the latest entry on file and reports `saved=false` + `source=carried_over`
// so the frontend can prompt the user to confirm/save for today. When nothing
// has ever been saved, returns the empty projection.
func (s *Service) GetCurrent(ctx context.Context, userID uuid.UUID) (dto.RoutineResponse, error) {
	var zero dto.RoutineResponse
	if s == nil || s.routines == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	today := todayUTC()

	row, err := s.routines.GetByUserAndDate(ctx, userID, today)
	if err != nil {
		return zero, err
	}
	if row != nil {
		return dto.RoutineFromDomain(row, true), nil
	}

	latest, err := s.routines.GetLatestForUser(ctx, userID)
	if err != nil {
		return zero, err
	}
	if latest == nil {
		// True empty state: try seeding from the onboarding starter routine so
		// the user lands on something useful instead of a blank slate. This is
		// best-effort — if we can't decode it, we return the empty projection.
		if seeded, ok := s.seedFromStarter(ctx, userID); ok {
			return seeded, nil
		}
		return dto.EmptyRoutineResponse(userID), nil
	}
	out := dto.RoutineFromDomain(latest, false)
	out.RoutineDate = today.UTC().Format("2006-01-02")
	out.UserID = userID.String()
	return out, nil
}

// Upsert validates the request and writes today's routine. When `routine_date`
// is omitted, today (UTC) is used — making the "tick a step complete" use case
// a single POST without the frontend having to know the date.
func (s *Service) Upsert(ctx context.Context, userID uuid.UUID, req dto.PutRoutineRequest) (dto.RoutineResponse, error) {
	var zero dto.RoutineResponse
	if s == nil || s.routines == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("%w: user id required", ErrInvalidInput)
	}

	day, err := parseRoutineDate(req.RoutineDate)
	if err != nil {
		return zero, err
	}

	morning := sanitizeSteps(req.Morning)
	evening := sanitizeSteps(req.Evening)

	if len(morning) == 0 && len(evening) == 0 {
		return zero, fmt.Errorf("%w: morning and evening cannot both be empty", ErrInvalidInput)
	}

	morningJSON, err := json.Marshal(morning)
	if err != nil {
		return zero, err
	}
	eveningJSON, err := json.Marshal(evening)
	if err != nil {
		return zero, err
	}

	entry := &domain.RoutineEntry{
		UserID:      userID,
		RoutineDate: day,
		Morning:     morningJSON,
		Evening:     eveningJSON,
		Notes:       strings.TrimSpace(req.Notes),
		Source:      normalizeSource(req.Source),
		SkillMode:   strings.ToLower(strings.TrimSpace(req.SkillMode)),
	}

	saved, err := s.routines.UpsertForDay(ctx, entry)
	if err != nil {
		return zero, err
	}
	// Routine adherence is one of the memory block's inputs. Bust so the
	// next AI call sees the freshly-ticked steps and the digest tier
	// (low → moderate → strong) recalculates immediately.
	s.cache.Bust(userID)
	return dto.RoutineFromDomain(saved, true), nil
}

// History returns up to `rangeDays` (1..365) past entries, newest first, plus
// a streak count and average completion ratio for the summary card.
func (s *Service) History(ctx context.Context, userID uuid.UUID, rangeDays int) (dto.RoutineHistoryResponse, error) {
	var zero dto.RoutineHistoryResponse
	if s == nil || s.routines == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	if rangeDays <= 0 {
		rangeDays = 30
	}
	if rangeDays > 365 {
		rangeDays = 365
	}
	since := todayUTC().AddDate(0, 0, -rangeDays+1)
	rows, err := s.routines.ListForUserSince(ctx, userID, since, rangeDays)
	if err != nil {
		return zero, err
	}

	entries := make([]dto.RoutineResponse, 0, len(rows))
	for i := range rows {
		entries = append(entries, dto.RoutineFromDomain(&rows[i], true))
	}

	streak := computeStreak(rows)
	avg := computeCompletionAvg(entries)

	to := todayUTC().Format("2006-01-02")
	out := dto.RoutineHistoryResponse{
		RangeDays:     rangeDays,
		From:          since.Format("2006-01-02"),
		To:            to,
		Entries:       entries,
		StreakDays:    streak,
		CompletionAvg: avg,
	}
	return out, nil
}

// Suggest calls the AI service to build a fresh routine. Profile is required
// (the AI needs at least baseline skin info); a nil last check is OK.
func (s *Service) Suggest(ctx context.Context, userID uuid.UUID, req dto.SuggestRoutineRequest) (dto.SuggestRoutineResponse, error) {
	var zero dto.SuggestRoutineResponse
	if s == nil || s.routines == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("%w: user id required", ErrInvalidInput)
	}

	var profile *domain.SkinProfile
	if s.profiles != nil {
		p, err := s.profiles.GetByUserID(ctx, userID)
		if err == nil {
			profile = p
		}
	}

	var lastCheck *domain.SkinCheck
	var lastCheckID uuid.UUID
	if s.skinCheck != nil {
		// Pull the most recent check; ListRecentForCoach is designed for the
		// coach loop and returns minimal columns, which is fine here — the AI
		// only needs tags/symptoms/note for context.
		recent, err := s.skinCheck.ListRecentForCoach(ctx, userID, uuid.Nil, 1)
		if err == nil && len(recent) > 0 {
			lastCheck = &recent[0]
			lastCheckID = lastCheck.ID
		}
	}

	// USER_MEMORY — full SkinProfile + 5-8 recent check-ins with prior AI
	// summary lines + thumbs-up/down votes + routine adherence rate. We
	// exclude the "last check" id so the model doesn't see it twice (it's
	// already injected explicitly above as MOST_RECENT_CHECK_IN).
	//
	// Cache is intentionally NOT passed when ExcludeCheckID is set —
	// caching by exclude variant would either be wrong or require keyed
	// entries (see BuildUserMemoryContext docs).
	memDeps := ai.UserMemoryDeps{
		Profiles: s.profiles,
		Checks:   s.skinCheck,
		Feedback: s.feedback,
		Routines: s.routines,
	}
	if lastCheckID == uuid.Nil {
		memDeps.Cache = s.cache
	}
	memory, memDebug := ai.BuildUserMemoryWithDebug(
		ctx,
		userID,
		memDeps,
		ai.UserMemoryOptions{ExcludeCheckID: lastCheckID},
	)
	ai.LogMemoryInjection("routine-suggest", userID, uuid.Nil, memDebug)

	in := ai.SuggestRoutineInput{
		Profile:    profile,
		LastCheck:  lastCheck,
		Locale:     req.Locale,
		SkillMode:  req.SkillMode,
		FocusNote:  req.FocusNote,
		UserMemory: memory,
	}

	res, err := ai.GenerateSuggestedRoutine(ctx, s.cfg, in)
	if err != nil {
		return zero, err
	}

	skillMode := req.SkillMode
	if skillMode == "" && profile != nil {
		skillMode = string(profile.SkillLevel)
	}

	return dto.SuggestRoutineResponse{
		Morning:         res.Morning,
		Evening:         res.Evening,
		Encouragement:   res.Encouragement,
		Rationale:       res.Rationale,
		WeekNotes:       res.WeekNotes,
		SafetyNotes:     res.SafetyNotes,
		ClosingReminder: res.ClosingReminder,
		SkillMode:       skillMode,
		Locale:          strings.ToLower(strings.TrimSpace(req.Locale)),
		Source:          "ai_suggested",
		// Suggestion isn't persisted server-side, but the UI still needs a
		// stable id for any thumbs-up/down votes attached to *this* draft.
		FeedbackTargetID: uuid.New().String(),
	}, nil
}

// seedFromStarter promotes the onboarding starter routine (stored in the
// SkinProfile's onboarding snapshot) into a "carried_over" routine response
// when the user has never saved anything. This way, the /routine page is
// instantly populated after onboarding without a server-side write.
func (s *Service) seedFromStarter(ctx context.Context, userID uuid.UUID) (dto.RoutineResponse, bool) {
	if s.profiles == nil {
		return dto.RoutineResponse{}, false
	}
	prof, err := s.profiles.GetByUserID(ctx, userID)
	if err != nil || prof == nil || len(prof.OnboardingSnapshot) == 0 {
		return dto.RoutineResponse{}, false
	}
	var snap map[string]any
	if err := json.Unmarshal(prof.OnboardingSnapshot, &snap); err != nil || snap == nil {
		return dto.RoutineResponse{}, false
	}
	starter, ok := snap["starter_routine"].(map[string]any)
	if !ok {
		return dto.RoutineResponse{}, false
	}
	morning := convertStarterList(starter["morning"])
	evening := convertStarterList(starter["evening"])
	if len(morning) == 0 && len(evening) == 0 {
		return dto.RoutineResponse{}, false
	}
	return dto.RoutineResponse{
		UserID:      userID.String(),
		RoutineDate: todayUTC().Format("2006-01-02"),
		Morning:     morning,
		Evening:     evening,
		Source:      "carried_over",
		Saved:       false,
	}, true
}

func convertStarterList(v any) []dto.RoutineStep {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]dto.RoutineStep, 0, len(arr))
	for _, line := range arr {
		s, ok := line.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, dto.RoutineStep{
			ID:    uuid.New().String(),
			Title: s,
		})
	}
	return out
}

func sanitizeSteps(steps []dto.RoutineStep) []dto.RoutineStep {
	if len(steps) == 0 {
		return []dto.RoutineStep{}
	}
	out := make([]dto.RoutineStep, 0, len(steps))
	for _, s := range steps {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			continue
		}
		id := strings.TrimSpace(s.ID)
		if id == "" {
			id = uuid.New().String()
		}
		out = append(out, dto.RoutineStep{
			ID:        id,
			Title:     title,
			Category:  strings.ToLower(strings.TrimSpace(s.Category)),
			Notes:     strings.TrimSpace(s.Notes),
			Completed: s.Completed,
		})
	}
	return out
}

func normalizeSource(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ai_suggested":
		return "ai_suggested"
	case "carried_over":
		return "carried_over"
	default:
		return "manual"
	}
}

func parseRoutineDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return todayUTC(), nil
	}
	d, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: routine_date must be YYYY-MM-DD", ErrInvalidInput)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC), nil
}

func todayUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// computeStreak counts the number of consecutive most-recent days (starting
// from today) where the entry had at least one step ticked complete.
// rows are expected ordered newest-first.
func computeStreak(rows []domain.RoutineEntry) int {
	if len(rows) == 0 {
		return 0
	}
	expected := todayUTC()
	streak := 0
	for _, r := range rows {
		d := r.RoutineDate.UTC()
		d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		if !d.Equal(expected) {
			break
		}
		if hasAnyCompleted(r.Morning) || hasAnyCompleted(r.Evening) {
			streak++
			expected = expected.AddDate(0, 0, -1)
			continue
		}
		break
	}
	return streak
}

func hasAnyCompleted(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var steps []dto.RoutineStep
	if err := json.Unmarshal(raw, &steps); err != nil {
		return false
	}
	for _, s := range steps {
		if s.Completed {
			return true
		}
	}
	return false
}

func computeCompletionAvg(entries []dto.RoutineResponse) float64 {
	if len(entries) == 0 {
		return 0
	}
	var ratios []float64
	for _, e := range entries {
		total := len(e.Morning) + len(e.Evening)
		if total == 0 {
			continue
		}
		done := 0
		for _, s := range e.Morning {
			if s.Completed {
				done++
			}
		}
		for _, s := range e.Evening {
			if s.Completed {
				done++
			}
		}
		ratios = append(ratios, float64(done)/float64(total))
	}
	if len(ratios) == 0 {
		return 0
	}
	var sum float64
	for _, r := range ratios {
		sum += r
	}
	return sum / float64(len(ratios))
}
