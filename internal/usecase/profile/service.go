// Package profile implements skin profile & onboarding completion use cases.
package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	ErrInvalidInput          = errors.New("invalid profile payload")
	ErrUnavailable           = errors.New("profile service unavailable")
	ErrOnboardingNotFound    = errors.New("onboarding profile not found")
)

// Service coordinates SkinProfile persistence and onboarding + starter routine.
//
// The `checks`, `feedback`, `routines`, and `cache` fields are optional and
// only used when the user is RE-onboarding (i.e. already has history on
// file). Wired this way so a fresh-install API node can still serve
// CompleteOnboarding even when those repos aren't constructed.
type Service struct {
	cfg      *config.Config
	prof     *repository.GormSkinProfileRepository
	checks   *repository.GormSkinCheckRepository
	feedback *repository.GormAIFeedbackRepository
	routines *repository.GormRoutineEntryRepository
	wardrobe *repository.GormSkincareProductRepository
	cache    *ai.MemoryCache
}

// NewService wires dependencies. Pass nil for any of the optional history
// repos / cache — the starter routine will fall back to first-time mode.
func NewService(
	cfg *config.Config,
	prof *repository.GormSkinProfileRepository,
	checks *repository.GormSkinCheckRepository,
	feedback *repository.GormAIFeedbackRepository,
	routines *repository.GormRoutineEntryRepository,
	wardrobe *repository.GormSkincareProductRepository,
	cache *ai.MemoryCache,
) *Service {
	return &Service{
		cfg:      cfg,
		prof:     prof,
		checks:   checks,
		feedback: feedback,
		routines: routines,
		wardrobe: wardrobe,
		cache:    cache,
	}
}

// GetSkin returns the user's skin profile or a minimal empty projection.
func (s *Service) GetSkin(ctx context.Context, userID uuid.UUID) (dto.SkinProfileResponse, error) {
	var zero dto.SkinProfileResponse
	if s == nil || s.prof == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	p, err := s.prof.GetByUserID(ctx, userID)
	if err != nil {
		return zero, err
	}
	if p == nil {
		return dto.SkinProfileResponse{
			UserID:     userID.String(),
			SkillLevel: string(domain.SkillLevelUnspecified),
			Version:    1,
		}, nil
	}
	res := dto.SkinProfileFromDomain(p)
	res.OnboardingSnapshot = s.enrichStarterAffiliateSnapshot(ctx, userID, res.OnboardingSnapshot)
	return res, nil
}

// PutSkin applies a partial manual update (does not call AI).
func (s *Service) PutSkin(ctx context.Context, userID uuid.UUID, req dto.PutSkinProfileRequest) (dto.SkinProfileResponse, error) {
	var zero dto.SkinProfileResponse
	if s == nil || s.prof == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	existing, err := s.prof.GetByUserID(ctx, userID)
	if err != nil {
		return zero, err
	}
	p := existing
	if p == nil {
		p = &domain.SkinProfile{UserID: userID, SkillLevel: domain.SkillLevelUnspecified}
	}
	if req.SkinType != nil {
		p.SkinType = strings.TrimSpace(*req.SkinType)
	}
	if req.SkillLevel != nil {
		p.SkillLevel = parseSkillLevel(*req.SkillLevel)
	}
	if req.Notes != nil {
		p.Notes = strings.TrimSpace(*req.Notes)
	}
	if req.HomeCountryCode != nil {
		p.HomeCountryCode = strings.ToUpper(strings.TrimSpace(*req.HomeCountryCode))
		if len(p.HomeCountryCode) > 2 {
			p.HomeCountryCode = p.HomeCountryCode[:2]
		}
	}
	if req.ClimateZone != nil {
		p.ClimateZone = strings.TrimSpace(*req.ClimateZone)
	}
	if len(req.Concerns) > 0 {
		b, err := json.Marshal(req.Concerns)
		if err != nil {
			return zero, err
		}
		p.Concerns = b
	}
	if len(req.OnboardingSnapshot) > 0 {
		p.OnboardingSnapshot = append(json.RawMessage(nil), req.OnboardingSnapshot...)
	}
	if err := s.prof.UpsertForUser(ctx, p); err != nil {
		return zero, err
	}
	// Profile is a primary input to BuildUserMemoryContext — bust so the
	// next AI call sees the new tags / skill level immediately instead of
	// waiting out the TTL.
	s.cache.Bust(userID)
	reloaded, err := s.prof.GetByUserID(ctx, userID)
	if err != nil || reloaded == nil {
		return zero, fmt.Errorf("%w: reload profile", ErrUnavailable)
	}
	return dto.SkinProfileFromDomain(reloaded), nil
}

// CompleteOnboarding validates onboarding answers, persists SkinProfile, calls AI for starter routine.
// photoRels are relative upload paths (e.g. userID/onboarding/file.jpg); empty when photos were skipped.
func (s *Service) CompleteOnboarding(ctx context.Context, userID uuid.UUID, req dto.OnboardingCompleteRequest, photoRels []string) (dto.OnboardingCompleteResponse, error) {
	var zero dto.OnboardingCompleteResponse
	if s == nil || s.prof == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("%w: user id required", ErrInvalidInput)
	}
	if strings.TrimSpace(req.SkinType) == "" || strings.TrimSpace(req.Goal) == "" || strings.TrimSpace(req.SkillLevel) == "" {
		return zero, fmt.Errorf("%w: skin_type, goal, and skill_level are required", ErrInvalidInput)
	}
	if strings.TrimSpace(req.Undertone) == "" {
		return zero, fmt.Errorf("%w: undertone is required", ErrInvalidInput)
	}
	budget := strings.TrimSpace(req.Budget)
	if budget == "" {
		budget = "mid"
	}
	contexts := req.Contexts
	if contexts == nil {
		contexts = []string{}
	}

	snap := map[string]any{
		"undertone":       req.Undertone,
		"contexts":        contexts,
		"budget":          budget,
		"goal":            req.Goal,
		"skin_type":       strings.TrimSpace(req.SkinType),
		"skill_level":     strings.TrimSpace(req.SkillLevel),
		"body_concerns":   req.BodyConcerns,
		"current_routine": strings.TrimSpace(req.CurrentRoutine),
		"completed_via":   "onboarding_v1",
		"locale":          onboardingLocale(req.Locale),
		"photos_skipped":  req.PhotosSkipped,
	}
	if req.SkinAnalysis != nil {
		snap["skin_analysis"] = req.SkinAnalysis
	}
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return zero, err
	}

	loc := onboardingLocale(req.Locale)

	// Respond instantly: persist a quick local scaffold now and let the LLM
	// generate the personalized routine in the background. The client shows
	// the scaffold immediately and polls GET /profile/skin until the AI
	// routine replaces it (starter_routine_pending flips to false).
	starter := quickStarterFromOnboarding(req, loc)
	snap["starter_routine"] = starter
	snap["starter_routine_pending"] = true

	var photoJSON json.RawMessage
	if len(photoRels) > 0 {
		b, err := json.Marshal(photoRels)
		if err != nil {
			return zero, err
		}
		photoJSON = b
		snap["photo_urls"] = dto.BuildPublicUploadURLs(photoJSON)
	}

	fullSnap, err := json.Marshal(snap)
	if err != nil {
		return zero, err
	}

	concernTags := []string{req.Goal, req.SkinType}
	concernTags = append(concernTags, req.BodyConcerns...)
	concernsJSON, _ := json.Marshal(concernTags)

	prof := &domain.SkinProfile{
		UserID:             userID,
		SkinType:           strings.TrimSpace(req.SkinType),
		SkillLevel:         parseSkillLevel(req.SkillLevel),
		Concerns:           concernsJSON,
		OnboardingSnapshot: fullSnap,
		PhotoURLs:          photoJSON,
	}
	if code := strings.TrimSpace(req.HomeCountryCode); code != "" {
		c := strings.ToUpper(code)
		if len(c) > 2 {
			c = c[:2]
		}
		prof.HomeCountryCode = c
	}
	if err := s.prof.UpsertForUser(ctx, prof); err != nil {
		return zero, err
	}
	// Onboarding completion materially changes the SkinProfile (and may
	// rewrite the starter routine inside the onboarding snapshot). Bust
	// the cache so the very next AI call after onboarding sees the new
	// profile instead of the pre-onboarding zero state.
	s.cache.Bust(userID)

	// Profile is persisted — safe to kick off the background AI refresh now
	// without racing the initial upsert. Memory is built inside the goroutine
	// so the HTTP response is not blocked on multiple DB reads.
	s.enqueueStarterRoutineRefresh(userID, snapJSON, loc)

	out := dto.OnboardingCompleteResponse{
		Profile:               dto.SkinProfileFromDomain(prof),
		StarterRoutine:        starterRoutineResponseFromAI(starter),
		StarterRoutinePending: true,
	}
	return out, nil
}

func (s *Service) memoryForStarterPrompt(ctx context.Context, userID uuid.UUID) string {
	if s == nil {
		return ""
	}
	memory, memDebug := ai.BuildUserMemoryWithDebug(
		ctx,
		userID,
		ai.UserMemoryDeps{
			Profiles: s.prof,
			Checks:   s.checks,
			Feedback: s.feedback,
			Routines: s.routines,
			Wardrobe: s.wardrobe,
			Cache:    s.cache,
		},
		ai.UserMemoryOptions{},
	)
	if !hasMeaningfulHistory(memory) {
		return ""
	}
	ai.LogMemoryInjection("starter-routine", userID, uuid.Nil, memDebug)
	return memory
}

func (s *Service) enqueueStarterRoutineRefresh(userID uuid.UUID, snapJSON []byte, loc string) {
	if s == nil || s.prof == nil || userID == uuid.Nil {
		return
	}
	payload := append([]byte(nil), snapJSON...)
	go func(uid uuid.UUID, snap []byte, locale string) {
		ctx, cancel := context.WithTimeout(context.Background(), starterRoutineBGTimeout)
		defer cancel()
		memoryForAI := s.memoryForStarterPrompt(ctx, uid)
		starter, err := ai.GenerateStarterRoutine(ctx, s.cfg, snap, locale, memoryForAI)
		if err != nil {
			slog.Warn("onboarding: background starter routine failed",
				"user_id", uid,
				"err", err,
			)
			if clrErr := s.clearStarterRoutinePending(ctx, uid); clrErr != nil {
				slog.Warn("onboarding: clear starter pending failed",
					"user_id", uid,
					"err", clrErr,
				)
			}
			return
		}
		if err := s.patchProfileStarterRoutine(ctx, uid, starter); err != nil {
			slog.Warn("onboarding: patch starter routine failed",
				"user_id", uid,
				"err", err,
			)
			return
		}
		s.cache.Bust(uid)
		slog.Info("onboarding: background starter routine ready", "user_id", uid)
	}(userID, payload, loc)
}

func (s *Service) clearStarterRoutinePending(ctx context.Context, userID uuid.UUID) error {
	if s == nil || s.prof == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	p, err := s.prof.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("%w: profile not found", ErrUnavailable)
	}
	var snap map[string]any
	if len(p.OnboardingSnapshot) == 0 {
		return nil
	}
	if err := json.Unmarshal(p.OnboardingSnapshot, &snap); err != nil {
		return err
	}
	if snap["starter_routine_pending"] == nil {
		return nil
	}
	delete(snap, "starter_routine_pending")
	fullSnap, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	p.OnboardingSnapshot = fullSnap
	return s.prof.UpsertForUser(ctx, p)
}

func (s *Service) patchProfileStarterRoutine(ctx context.Context, userID uuid.UUID, starter ai.StarterRoutine) error {
	if s == nil || s.prof == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	p, err := s.prof.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("%w: profile not found", ErrUnavailable)
	}
	var snap map[string]any
	if len(p.OnboardingSnapshot) > 0 {
		if err := json.Unmarshal(p.OnboardingSnapshot, &snap); err != nil {
			return err
		}
	} else {
		snap = map[string]any{}
	}
	snap["starter_routine"] = starter
	delete(snap, "starter_routine_pending")
	fullSnap, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	p.OnboardingSnapshot = fullSnap
	return s.prof.UpsertForUser(ctx, p)
}

// DeleteOnboarding removes the user's skin profile and carried-over starter routines.
func (s *Service) DeleteOnboarding(ctx context.Context, userID uuid.UUID) (dto.DeleteOnboardingResponse, error) {
	var zero dto.DeleteOnboardingResponse
	if s == nil || s.prof == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("%w: user id required", ErrInvalidInput)
	}
	existing, err := s.prof.GetByUserID(ctx, userID)
	if err != nil {
		return zero, err
	}
	if existing == nil || !hasOnboardingData(existing) {
		return zero, fmt.Errorf("%w", ErrOnboardingNotFound)
	}
	if s.routines != nil {
		if err := s.routines.DeleteCarriedOverByUserID(ctx, userID); err != nil {
			return zero, err
		}
	}
	if err := s.prof.DeleteByUserID(ctx, userID); err != nil {
		return zero, err
	}
	s.cache.Bust(userID)
	return dto.DeleteOnboardingResponse{
		DeletedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func hasOnboardingData(p *domain.SkinProfile) bool {
	if p == nil {
		return false
	}
	if strings.TrimSpace(p.SkinType) == "" && len(p.OnboardingSnapshot) == 0 {
		return false
	}
	if len(p.OnboardingSnapshot) == 0 {
		return strings.TrimSpace(p.SkinType) != ""
	}
	var snap map[string]any
	if err := json.Unmarshal(p.OnboardingSnapshot, &snap); err != nil {
		return len(p.OnboardingSnapshot) > 0
	}
	if via, _ := snap["completed_via"].(string); strings.TrimSpace(via) != "" {
		return true
	}
	if sr, ok := snap["starter_routine"]; ok && sr != nil {
		return true
	}
	return strings.TrimSpace(p.SkinType) != ""
}

// PreviewOnboardingComplete enqueues a guest starter routine job and returns a quick scaffold immediately.
func (s *Service) PreviewOnboardingComplete(ctx context.Context, req dto.OnboardingCompleteRequest) (dto.OnboardingPreviewResponse, error) {
	var zero dto.OnboardingPreviewResponse
	if s == nil {
		return zero, fmt.Errorf("%w", ErrUnavailable)
	}
	if strings.TrimSpace(req.SkinType) == "" || strings.TrimSpace(req.Goal) == "" || strings.TrimSpace(req.SkillLevel) == "" {
		return zero, fmt.Errorf("%w: skin_type, goal, and skill_level are required", ErrInvalidInput)
	}
	if strings.TrimSpace(req.Undertone) == "" {
		return zero, fmt.Errorf("%w: undertone is required", ErrInvalidInput)
	}
	budget := strings.TrimSpace(req.Budget)
	if budget == "" {
		budget = "mid"
	}
	contexts := req.Contexts
	if contexts == nil {
		contexts = []string{}
	}

	snap := map[string]any{
		"undertone":       req.Undertone,
		"contexts":        contexts,
		"budget":          budget,
		"goal":            req.Goal,
		"skin_type":       strings.TrimSpace(req.SkinType),
		"skill_level":     strings.TrimSpace(req.SkillLevel),
		"body_concerns":   req.BodyConcerns,
		"current_routine": strings.TrimSpace(req.CurrentRoutine),
		"completed_via":   "onboarding_guest_preview",
		"locale":          onboardingLocale(req.Locale),
	}
	if req.SkinAnalysis != nil {
		snap["skin_analysis"] = req.SkinAnalysis
	}
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return zero, err
	}

	loc := onboardingLocale(req.Locale)
	quick := quickStarterFromOnboarding(req, loc)
	quickDTO := starterRoutineResponseFromAI(quick)
	jobID := newPreviewJobID()
	storePreviewJob(jobID, quickDTO)

	go func(payload []byte, locale, id string, fallback dto.StarterRoutineResponse) {
		ctx, cancel := context.WithTimeout(context.Background(), starterRoutineBGTimeout)
		defer cancel()
		starter, err := ai.GenerateStarterRoutine(ctx, s.cfg, payload, locale, "")
		if err != nil {
			slog.Warn("onboarding: guest preview starter routine failed", "preview_job_id", id, "err", err)
			failPreviewJob(id, fallback)
			return
		}
		finishPreviewJob(id, starterRoutineResponseFromAI(starter))
		slog.Info("onboarding: guest preview starter routine ready", "preview_job_id", id)
	}(snapJSON, loc, jobID, quickDTO)

	return dto.OnboardingPreviewResponse{
		StarterRoutine:        quickDTO,
		StarterRoutinePending: true,
		PreviewJobID:          jobID,
	}, nil
}

// hasMeaningfulHistory returns true when BuildUserMemoryContext produced any
// real section (profile, recent checks, feedback, or routine adherence).
//
// We detect "empty" memory by the sentinel line emitted when no repos return
// data. Cheap, single substring scan — no parsing.
func hasMeaningfulHistory(memory string) bool {
	if strings.TrimSpace(memory) == "" {
		return false
	}
	if strings.Contains(memory, "no saved memory yet") {
		return false
	}
	return strings.Contains(memory, "##")
}

func parseSkillLevel(raw string) domain.SkillLevel {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "beginner":
		return domain.SkillLevelBeginner
	case "intermediate":
		return domain.SkillLevelIntermediate
	case "advanced":
		return domain.SkillLevelAdvanced
	default:
		return domain.SkillLevelUnspecified
	}
}

func (s *Service) enrichStarterAffiliateSnapshot(ctx context.Context, userID uuid.UUID, snap json.RawMessage) json.RawMessage {
	if len(snap) == 0 {
		return snap
	}
	loc := ai.LocaleFromOnboardingSnapshot(snap)
	return ai.EnrichOnboardingSnapshotStarterAffiliate(snap, loc, s.listOwnedWardrobe(ctx, userID))
}

func (s *Service) listOwnedWardrobe(ctx context.Context, userID uuid.UUID) []ai.OwnedWardrobeItem {
	if s == nil || s.wardrobe == nil || userID == uuid.Nil {
		return nil
	}
	rows, err := s.wardrobe.ListByUser(ctx, userID)
	if err != nil || len(rows) == 0 {
		return nil
	}
	out := make([]ai.OwnedWardrobeItem, 0, len(rows))
	for _, p := range rows {
		out = append(out, ai.OwnedWardrobeItem{
			Name:     p.Name,
			Brand:    p.Brand,
			Category: string(p.Category),
		})
	}
	return out
}

func onboardingLocale(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "en":
		return "en"
	case "vi":
		return "vi"
	default:
		return "vi"
	}
}

func fallbackStarterRoutine(locale string) ai.StarterRoutine {
	if locale == "en" {
		return ai.StarterRoutine{
			Encouragement:   "",
			SkinReadback:    "",
			Morning:         []string{},
			Evening:         []string{},
			Rationale:       "",
			WeekNotes:       "Starter routine could not be generated (AI unavailable). You can edit your profile and try again.",
			SafetyNotes:     "This is general skincare guidance, not a substitute for medical advice.",
			ClosingReminder: "Skin changes day to day — track gently, and see a dermatologist when something worries you.",
		}
	}
	return ai.StarterRoutine{
		Encouragement:   "",
		SkinReadback:    "",
		Morning:         []string{},
		Evening:         []string{},
		Rationale:       "",
		WeekNotes:       "Chưa tạo được Starter Routine (AI tạm không khả dụng). Bạn có thể chỉnh hồ sơ và thử hoàn tất onboarding lại.",
		SafetyNotes:     "Đây chỉ là gợi ý chăm sóc da; không thay thế tư vấn y tế.",
		ClosingReminder: "Da thay đổi theo ngày — theo dõi nhẹ nhàng và hỏi bác sĩ da liễu khi bạn lo lắng.",
	}
}
