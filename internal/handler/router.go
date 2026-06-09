package handler

import (
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/dadiary/backend/internal/service/analysis"
	"github.com/dadiary/backend/internal/service/moderation"
	"github.com/dadiary/backend/internal/token"
	aifeedbackuc "github.com/dadiary/backend/internal/usecase/aifeedback"
	affiliateuc "github.com/dadiary/backend/internal/usecase/affiliate"
	authuc "github.com/dadiary/backend/internal/usecase/auth"
	profileuc "github.com/dadiary/backend/internal/usecase/profile"
	routineuc "github.com/dadiary/backend/internal/usecase/routine"
	skincheckuc "github.com/dadiary/backend/internal/usecase/skincheck"
	usageuc "github.com/dadiary/backend/internal/usecase/usage"
	usermemoryuc "github.com/dadiary/backend/internal/usecase/usermemory"
	userdatauc "github.com/dadiary/backend/internal/usecase/userdata"
	wardrobeuc "github.com/dadiary/backend/internal/usecase/wardrobe"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// AI rate-limit budgets per authenticated user.
//
// These caps are designed for normal usage (a handful of check-ins per day,
// occasional onboarding) while keeping vendor cost predictable and stopping
// scripted abuse. Tune via env if you ever want to expose these as config.
const (
	// Onboarding analyze: users often retake photos and retry analysis several
	// times during setup. Keep a cap for cost control, but avoid locking out
	// normal onboarding (6/hour was too tight in beta).
	onboardingAnalyzeRateMax    = 12
	onboardingAnalyzeRateWindow = 15 * time.Minute

	// Skin check create: combines moderation + multimodal vision; the most
	// expensive endpoint. Three a minute / ~30 an hour is plenty for daily
	// use without enabling abuse.
	skinCheckRateMax    = 3
	skinCheckRateWindow = time.Minute

	// Routine suggestion + onboarding complete: single LLM JSON call each;
	// users may iterate a few times when they don't love the first draft.
	routineSuggestRateMax    = 12
	routineSuggestRateWindow = time.Minute

	onboardingCompleteRateMax    = 6
	onboardingCompleteRateWindow = time.Minute
)

// Router wires API v1 routes: health (public), auth (mixed), skin-checks (protected).
func Router(app *fiber.App, cfg *config.Config, db *gorm.DB, tok *token.Service) {
	api := app.Group("/api/v1")
	NewHealthHandler(cfg).Register(api)

	// Per-route JWT only. Do NOT use api.Group("", jwt): in Fiber that registers USE on
	// /api/v1 and runs auth before public routes like POST /auth/register.
	jwt := middleware.RequireAccessJWT(tok)
	jwtOptional := middleware.OptionalAccessJWT(tok)

	var authSvc *authuc.Service
	if db != nil && tok != nil {
		authSvc = authuc.NewService(repository.NewUserRepository(db), tok)
	}
	authH := NewAuthHandler(authSvc, cfg)
	authH.RegisterRoutes(api, jwt)

	// Rate limiters scoped per authenticated user (falls back to IP for anon).
	// Order matters: limiter must run AFTER `jwt` so the key generator can
	// pick up the user UUID from locals; otherwise every request from one
	// IP would share a bucket.
	onboardingAnalyzeLimit := middleware.AILimiter(
		onboardingAnalyzeRateMax,
		onboardingAnalyzeRateWindow,
	)
	skinCheckLimit := middleware.AILimiter(
		skinCheckRateMax,
		skinCheckRateWindow,
	)
	routineSuggestLimit := middleware.AILimiter(
		routineSuggestRateMax,
		routineSuggestRateWindow,
	)
	onboardingCompleteLimit := middleware.AILimiter(
		onboardingCompleteRateMax,
		onboardingCompleteRateWindow,
	)

	if cfg != nil {
		oh := NewOnboardingAnalyzeHandler(cfg)
		api.Post("/onboarding/analyze-skin", jwtOptional, onboardingAnalyzeLimit, oh.AnalyzeSkin)
	}

	if cfg != nil && db != nil {
		repo := repository.NewSkinCheckRepository(db)
		profRepo := repository.NewSkinProfileRepository(db)
		// Feedback + routine repos are constructed early so both the
		// analysis service (daily check-in coach) and the routine service
		// (AI suggest) can inject the user's long-term memory — prior
		// thumbs-up/down votes AND routine adherence — into prompts.
		fbRepo := repository.NewAIFeedbackRepository(db)
		routineRepo := repository.NewRoutineEntryRepository(db)
		wardRepo := repository.NewSkincareProductRepository(db)
		affiliateRepo := repository.NewAffiliateClickRepository(db)
		// One in-process memory cache shared by all services that read or
		// invalidate the long-term USER_MEMORY block. 5-minute TTL +
		// explicit bust from each write path keeps results fresh without
		// pegging the DB on every AI call.
		memCache := ai.NewMemoryCache()
		userRepo := repository.NewUserRepository(db)
		usageSvc := usageuc.NewService(userRepo, repository.NewUsageEventRepository(db))
		usageH := NewMeUsageHandler(usageSvc)
		api.Get("/me/usage", jwt, usageH.Get)
		mod := moderation.New(cfg)
		analyzer := analysis.New(cfg, repo, profRepo, fbRepo, routineRepo, wardRepo, memCache)
		svc := skincheckuc.NewService(cfg, repo, mod, analyzer)
		h := NewSkinCheckHandler(svc, repo, cfg)
		api.Post("/skin-checks", jwt, skinCheckLimit, h.Create)
		api.Get("/skin-checks/:id", jwt, h.Get)

		// Progress Timeline + Summary — both read aggregations over skin_checks +
		// skin_analyses. No LLM call, so they stay snappy even on cold cache.
		progressH := NewProgressHandler(repo)
		api.Get("/progress", jwt, progressH.Timeline)
		api.Get("/progress/summary", jwt, progressH.Summary)

		// profileSvc carries the full repo + cache set so re-onboarding
		// can pull the user's existing memory and inject it into the
		// starter routine prompt — keeping the new starter coherent with
		// what the coach already knows about them.
		profSvc := profileuc.NewService(cfg, profRepo, repo, fbRepo, routineRepo, wardRepo, memCache)
		ph := NewProfileHandler(profSvc, cfg)
		api.Get("/profile/skin", jwt, ph.GetSkin)
		api.Put("/profile/skin", jwt, ph.PutSkin)
		api.Post(
			"/profile/onboarding/complete",
			jwt,
			onboardingCompleteLimit,
			ph.CompleteOnboarding,
		)
		api.Delete("/profile/onboarding", jwt, ph.DeleteOnboarding)
		api.Post(
			"/onboarding/preview-complete",
			jwtOptional,
			onboardingCompleteLimit,
			ph.PreviewOnboardingComplete,
		)
		api.Get(
			"/onboarding/preview-routine/:id",
			ph.GetPreviewRoutine,
		)

		wardSvc := wardrobeuc.NewService(wardRepo, memCache, usageSvc)
		wh := NewWardrobeHandler(wardSvc)
		api.Post("/wardrobe/products", jwt, wh.CreateProduct)
		api.Get("/wardrobe", jwt, wh.List)

		affiliateSvc := affiliateuc.NewService(affiliateRepo)
		affH := NewAffiliateHandler(affiliateSvc)
		api.Post("/affiliate/clicks", jwt, affH.LogClick)

		fbSvc := aifeedbackuc.NewService(fbRepo, memCache)
		fbh := NewAIFeedbackHandler(fbSvc)
		api.Post("/ai/feedback", jwt, fbh.Create)
		// History — let users (and the future "your votes" view) read back
		// their feedback. No rate limit: this is a cheap read.
		api.Get("/ai/feedback/me", jwt, fbh.List)

		// /me/memory — debug / inspect endpoint. Returns the same
		// USER_MEMORY block we inject into AI prompts plus a small
		// diagnostic block (char count, cache stats, history counts).
		// Useful for the frontend "what does the AI know about me?"
		// transparency view and for engineers verifying the prompt loop.
		memSvc := usermemoryuc.NewService(repo, profRepo, fbRepo, routineRepo, wardRepo, memCache)
		mh := NewMeMemoryHandler(memSvc)
		api.Get("/me/memory", jwt, mh.Get)

		userDataRepo := repository.NewUserDataRepository(db)
		uploadDir := ""
		if cfg != nil {
			uploadDir = cfg.Upload.Dir
		}
		userDataSvc := userdatauc.NewService(userDataRepo, uploadDir, memCache)
		mdh := NewMeDataHandler(userDataSvc)
		api.Delete("/me/data", jwt, mdh.Delete)

		// Routine Management — daily AM/PM skincare routines, AI suggestion,
		// and history for the progress view. The skinCheck repo is reused
		// here read-only so the AI suggest call can pull the user's most
		// recent check-in as context (tags / symptoms / situation). The
		// feedback repo lets the suggest prompt adapt to past votes; the
		// routine repo is reused below for adherence stats in the memory
		// builder; the memory cache is busted after every Upsert.
		routineSvc := routineuc.NewService(cfg, routineRepo, profRepo, repo, fbRepo, wardRepo, memCache, usageSvc)
		rh := NewRoutineHandler(routineSvc)
		api.Get("/routines", jwt, rh.GetCurrent)
		api.Post("/routines", jwt, rh.Put)
		api.Get("/routines/history", jwt, rh.History)
		api.Post("/routines/suggest", jwt, routineSuggestLimit, rh.Suggest)
	}
}
