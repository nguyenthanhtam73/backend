package handler

import (
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/dadiary/backend/internal/service/analysis"
	"github.com/dadiary/backend/internal/service/moderation"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/dadiary/backend/internal/storage"
	"github.com/dadiary/backend/internal/token"
	affiliateuc "github.com/dadiary/backend/internal/usecase/affiliate"
	aifeedbackuc "github.com/dadiary/backend/internal/usecase/aifeedback"
	adminmetricsuc "github.com/dadiary/backend/internal/usecase/adminmetrics"
	adminuseruc "github.com/dadiary/backend/internal/usecase/adminuser"
	authuc "github.com/dadiary/backend/internal/usecase/auth"
	betasignupuc "github.com/dadiary/backend/internal/usecase/betasignup"
	dashboarduc "github.com/dadiary/backend/internal/usecase/dashboard"
	feedbackuc "github.com/dadiary/backend/internal/usecase/feedback"
	paymentuc "github.com/dadiary/backend/internal/usecase/payment"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	profileuc "github.com/dadiary/backend/internal/usecase/profile"
	pushuc "github.com/dadiary/backend/internal/usecase/push"
	routineuc "github.com/dadiary/backend/internal/usecase/routine"
	skincheckuc "github.com/dadiary/backend/internal/usecase/skincheck"
	streakuc "github.com/dadiary/backend/internal/usecase/streak"
	subscriptionuc "github.com/dadiary/backend/internal/usecase/subscription"
	usageuc "github.com/dadiary/backend/internal/usecase/usage"
	userdatauc "github.com/dadiary/backend/internal/usecase/userdata"
	usermemoryuc "github.com/dadiary/backend/internal/usecase/usermemory"
	wardrobeuc "github.com/dadiary/backend/internal/usecase/wardrobe"
	"github.com/dadiary/backend/pkg/alert"
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
func Router(app *fiber.App, cfg *config.Config, db *gorm.DB, tok *token.Service, store storage.Storage) {
	api := app.Group("/api/v1")
	NewHealthHandler(cfg).Register(api)

	// Per-route JWT only. Do NOT use api.Group("", jwt): in Fiber that registers USE on
	// /api/v1 and runs auth before public routes like POST /auth/register.
	jwt := middleware.RequireAccessJWT(tok)
	jwtOptional := middleware.OptionalAccessJWT(tok)

	// Auth — Domain → AuthRepository → AuthUsecase → Handler
	var authSvc *authuc.Usecase
	if db != nil && tok != nil {
		authRepo := repository.NewAuthRepository(db)
		authSvc = authuc.NewUsecase(authRepo, tok)
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
		userUsageRepo := repository.NewUserUsageRepository(db)
		premiumSvc := premiumuc.NewService(userRepo, userUsageRepo)
		usageSvc := usageuc.NewWithGates(premiumSvc)
		usageH := NewMeUsageHandler(usageSvc)
		api.Get("/me/usage", jwt, usageH.Get)

		streakRepo := repository.NewStreakRepository(db)
		streakSvc := streakuc.NewService(streakRepo, repo)
		streakH := NewStreakHandler(streakSvc, premiumSvc)
		api.Get("/me/streak", jwt, streakH.Get)
		api.Get("/me/streak/milestones", jwt, streakH.Milestones)
		api.Post("/me/streak/freeze", jwt, streakH.UseFreeze)
		// Reconcile is intentionally NOT on /me/* — see AdminReconcile below.

		// Dashboard aggregate (home summary) — keeps /me/streak + /me/usage URLs intact.
		dashSvc := dashboarduc.NewUsecase(streakSvc, usageSvc, repo, premiumSvc)
		dashH := NewDashboardHandler(dashSvc)
		api.Get("/me/dashboard", jwt, dashH.GetSummary)

		mod := moderation.New(cfg)
		analyzer := analysis.New(cfg, repo, profRepo, fbRepo, routineRepo, wardRepo, memCache, store)
		txRunner := repository.NewTxRunner(db)
		svc := skincheckuc.NewService(cfg, repo, mod, analyzer, store, streakSvc, txRunner)
		h := NewSkinCheckHandler(svc, repo, cfg, premiumSvc)
		api.Post("/skin-checks", jwt, skinCheckLimit, h.Create)
		api.Get("/skin-checks/:id", jwt, h.Get)

		// Progress Timeline + Summary — both read aggregations over skin_checks +
		// skin_analyses. No LLM call, so they stay snappy even on cold cache.
		progressH := NewProgressHandler(repo, premiumSvc)
		api.Get("/progress", jwt, progressH.Timeline)
		api.Get("/progress/summary", jwt, progressH.Summary)

		// profileSvc carries the full repo + cache set so re-onboarding
		// can pull the user's existing memory and inject it into the
		// starter routine prompt — keeping the new starter coherent with
		// what the coach already knows about them.
		profSvc := profileuc.NewService(cfg, profRepo, repo, fbRepo, routineRepo, wardRepo, memCache)
		ph := NewProfileHandler(profSvc, cfg, store, premiumSvc)
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
		userDataSvc := userdatauc.NewService(userDataRepo, store, memCache, premiumSvc)
		mdh := NewMeDataHandler(userDataSvc)
		api.Get("/me/export", jwt, mdh.Export)
		api.Delete("/me/data", jwt, mdh.Delete)

		// Routine Management — daily AM/PM skincare routines, AI suggestion,
		// and history for the progress view. The skinCheck repo is reused
		// here read-only so the AI suggest call can pull the user's most
		// recent check-in as context (tags / symptoms / situation). The
		// feedback repo lets the suggest prompt adapt to past votes; the
		// routine repo is reused below for adherence stats in the memory
		// builder; the memory cache is busted after every Upsert.
		routineSvc := routineuc.NewService(cfg, routineRepo, profRepo, repo, fbRepo, wardRepo, memCache, usageSvc)
		rh := NewRoutineHandler(routineSvc, premiumSvc)
		api.Get("/routines", jwt, rh.GetCurrent)
		api.Post("/routines", jwt, rh.Put)
		api.Get("/routines/history", jwt, rh.History)
		api.Post("/routines/suggest", jwt, routineSuggestLimit, rh.Suggest)
		api.Get("/routines/suggest/status", jwt, rh.SuggestStatus)
		api.Delete("/routines/suggest", jwt, rh.CancelSuggest)

		// Web Push: subscribe lifecycle (Phase 1) + send infrastructure (Phase 2).
		pushRepo := repository.NewPushSubscriptionRepository(db)
		pushSender := pushsvc.NewPushSender(cfg, pushRepo)
		// Skin-check + streak repos power reminder filters (checked-in today / at risk).
		pushReceipts := repository.NewPushSendReceiptRepository(db)
		pushSvc := pushuc.NewService(pushRepo, pushSender, repo, streakRepo, pushReceipts)
		pushH := NewPushSubscriptionHandler(pushSvc)
		api.Post("/me/push/subscribe", jwt, pushH.Subscribe)
		api.Delete("/me/push/unsubscribe", jwt, pushH.Unsubscribe)
		api.Get("/me/push/subscription", jwt, pushH.GetActive)
		api.Post("/me/push/test", jwt, pushH.SendTest)

		// User product feedback (bugs, feature ideas) — distinct from AI thumbs.
		appFeedbackRepo := repository.NewFeedbackRepository(db)
		appFeedbackSvc := feedbackuc.NewService(appFeedbackRepo)
		appFeedbackH := NewFeedbackHandler(appFeedbackSvc)
		api.Post("/feedbacks", jwt, appFeedbackH.Create)

		// Admin triage console — gated by DADIARY_ADMIN_EMAILS allow-list
		// (exposed to clients as user.is_admin on GET /me).
		admin := middleware.RequireAdmin(cfg, userRepo)
		api.Get("/admin/feedbacks", jwt, admin, appFeedbackH.AdminList)
		api.Patch("/admin/feedbacks/:id", jwt, admin, appFeedbackH.AdminUpdateStatus)
		// Streak reconcile: repair drifted counters from SkinCheck history.
		// Admin-only — must never be a self-serve refill for freezes.
		api.Post("/admin/users/:userId/streak/reconcile", jwt, admin, streakH.AdminReconcile)

		// Internal plan grant/revoke for Premium testing.
		planLogRepo := repository.NewPlanChangeLogRepository(db)
		adminUserSvc := adminuseruc.NewService(db, userRepo, planLogRepo, cfg)
		adminUsersH := NewAdminUsersHandler(adminUserSvc)
		api.Get("/admin/users", jwt, admin, adminUsersH.List)
		api.Get("/admin/users/:id", jwt, admin, adminUsersH.Get)
		api.Put("/admin/users/:id/plan", jwt, admin, adminUsersH.UpdatePlan)

		// Payment / subscription monitoring dashboard (admin-only).
		payOrders := repository.NewPaymentOrderRepository(db)
		payOps := repository.NewPaymentOpsEventRepository(db)
		adminMetricsH := NewAdminMetricsHandler(adminmetricsuc.NewService(payOrders, userRepo, payOps))
		api.Get("/admin/metrics/payment", jwt, admin, adminMetricsH.Payment)

		// Public Beta waitlist — landing page email capture (no auth required).
		betaSignupRepo := repository.NewBetaSignupRepository(db)
		betaSignupSvc := betasignupuc.NewService(betaSignupRepo)
		betaSignupH := NewBetaSignupHandler(betaSignupSvc)
		api.Post("/beta-signups", betaSignupH.Create)

		// Subscription lifecycle (trial / cancel / renew / grace).
		subsRepo := repository.NewSubscriptionRepository(db)
		trialDays, graceDays := 7, 3
		if cfg != nil {
			trialDays = cfg.Subscription.TrialDays
			graceDays = cfg.Subscription.GraceDays
		}
		subSvc := subscriptionuc.NewService(db, userRepo, subsRepo, planLogRepo, trialDays, graceDays)
		authH.AttachSubscription(subSvc)
		subH := NewSubscriptionHandler(subSvc)
		api.Post("/subscription/cancel", jwt, subH.Cancel)

		// SePay Payment Gateway — checkout (JWT) + IPN webhook (public).
		// Configure IPN URL in SePay dashboard → POST /api/v1/payment/sepay/webhook
		// ORDER_PAID → SubscriptionService.ApplyRenewalTx (same DB transaction).
		paySvc := paymentuc.NewService(db, cfg, payOrders, userRepo, planLogRepo)
		paySvc.AttachSubscription(subSvc)
		var opsAlerter alert.Alerter
		var alertRec *alert.Recorder
		if cfg != nil {
			fanout := alert.New(alert.Config{
				Enabled:          cfg.Alert.Enabled,
				WebhookURL:       cfg.Alert.WebhookURL,
				TelegramBotToken: cfg.Alert.TelegramBotToken,
				TelegramChatID:   cfg.Alert.TelegramChatID,
			})
			// When E2E helpers are on, tee alerts into an in-memory Recorder so
			// Playwright can assert payment_success without real Telegram.
			if cfg.E2EHelpersEnabled() {
				alertRec = alert.NewRecorder(fanout)
				opsAlerter = alertRec
			} else {
				opsAlerter = fanout
			}
			paySvc.AttachAlerter(opsAlerter)
			paySvc.AttachMonitor(paymentuc.NewMonitor(opsAlerter, payOps))
		}
		payH := NewPaymentSePayHandler(paySvc)
		api.Post("/payment/sepay/checkout", jwt, payH.CreateCheckout)
		api.Post("/payment/sepay/webhook", payH.Webhook)

		// Playwright smoke helpers — only when DADIARY_E2E_SECRET is set.
		NewE2EHandler(cfg, db, userRepo).
			AttachAlertRecorder(alertRec).
			AttachOpsRepo(payOps).
			Register(api)
	}
}
