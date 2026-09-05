package main

import (
	"context"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/handler"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/scheduler"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/dadiary/backend/internal/storage"
	"github.com/dadiary/backend/internal/token"
	checkinreminderuc "github.com/dadiary/backend/internal/usecase/checkinreminder"
	paymentuc "github.com/dadiary/backend/internal/usecase/payment"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	pushuc "github.com/dadiary/backend/internal/usecase/push"
	subscriptionuc "github.com/dadiary/backend/internal/usecase/subscription"
	"github.com/dadiary/backend/pkg/alert"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	tok, err := token.NewService(cfg.JWT)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jwt: %v\n", err)
		os.Exit(1)
	}

	db, err := repository.NewPostgres(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v (API will still start; set DADIARY_DATABASE_URL for auth/skin-checks)\n", err)
	} else {
		if migErr := repository.AutoMigrate(db); migErr != nil {
			fmt.Fprintf(os.Stderr, "migrate: %v\n", migErr)
		}
	}

	app := fiber.New(fiber.Config{
		AppName:      "DaDiary API",
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		// Multipart skin photo uploads need a higher limit than Fiber's default 4MB.
		BodyLimit: 100 * 1024 * 1024,
	})

	middleware.RegisterDefault(app)

	store, err := storage.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "storage: %v\n", err)
		os.Exit(1)
	}
	registerUploadServing(app, store)

	handler.Router(app, cfg, db, tok, store)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Background jobs — cancelled with the same ctx that stops HTTP on SIGINT/SIGTERM.
	startDailyReminderJob(ctx, cfg, db)
	startMonthlyUsageResetJob(ctx, db)
	startPlanExpiryJob(ctx, cfg, db)
	startCheckInReminderJob(ctx, cfg, db)
	startPendingOrderExpiryJob(ctx, cfg, db)

	go func() {
		addr := fmt.Sprintf(":%d", cfg.HTTP.Port)
		if serveErr := app.Listen(addr); serveErr != nil {
			fmt.Fprintf(os.Stderr, "server: %v\n", serveErr)
			stop()
		}
	}()

	<-ctx.Done()
	_ = app.Shutdown()
}

// startDailyReminderJob wires push deps and starts the scheduler when enabled.
func startDailyReminderJob(ctx context.Context, cfg *config.Config, db *gorm.DB) {
	if cfg == nil || !cfg.DailyReminder.Enabled {
		slog.Info("daily_reminder_job: disabled via config")
		return
	}
	if db == nil {
		slog.Warn("daily_reminder_job: skipped — database not available")
		return
	}
	if !cfg.HasVAPIDKeys() {
		slog.Warn("daily_reminder_job: skipped — VAPID keys not configured")
		return
	}

	pushRepo := repository.NewPushSubscriptionRepository(db)
	pushSender := pushsvc.NewPushSender(cfg, pushRepo)
	skinCheckRepo := repository.NewSkinCheckRepository(db)
	streakRepo := repository.NewStreakRepository(db)
	pushReceipts := repository.NewPushSendReceiptRepository(db)
	pushSvc := pushuc.NewService(pushRepo, pushSender, skinCheckRepo, streakRepo, pushReceipts)
	jobLocks := repository.NewPushJobLockRepository(db)
	scheduler.NewDailyReminderJob(pushSvc, cfg, jobLocks).Start(ctx)
}

// startMonthlyUsageResetJob cleans completed user_usages rows on the 1st UTC.
func startMonthlyUsageResetJob(ctx context.Context, db *gorm.DB) {
	if db == nil {
		slog.Warn("monthly_usage_reset_job: skipped — database not available")
		return
	}
	userRepo := repository.NewUserRepository(db)
	usageRepo := repository.NewUserUsageRepository(db)
	premiumSvc := premiumuc.NewService(userRepo, usageRepo)
	jobLocks := repository.NewPushJobLockRepository(db)
	scheduler.NewMonthlyUsageResetJob(premiumSvc, jobLocks).Start(ctx)
}

// startPlanExpiryJob downgrades users whose grace window ended (daily UTC).
// Prefers SubscriptionService (history + status) when available; falls back to
// premium.DowngradeExpiredPlans for the same grace-aware cutoff.
func startPlanExpiryJob(ctx context.Context, cfg *config.Config, db *gorm.DB) {
	if db == nil {
		slog.Warn("plan_expiry_job: skipped — database not available")
		return
	}
	userRepo := repository.NewUserRepository(db)
	usageRepo := repository.NewUserUsageRepository(db)
	logs := repository.NewPlanChangeLogRepository(db)
	subsRepo := repository.NewSubscriptionRepository(db)
	graceDays := domain.DefaultGraceDays
	trialDays := domain.DefaultTrialDays
	if cfg != nil {
		graceDays = cfg.Subscription.GraceDays
		trialDays = cfg.Subscription.TrialDays
	}
	premiumSvc := premiumuc.NewService(userRepo, usageRepo)
	premiumSvc.AttachPlanExpiryDeps(db, userRepo, logs, graceDays)
	subSvc := subscriptionuc.NewService(db, userRepo, subsRepo, logs, trialDays, graceDays)
	// Boot reconcile does not use the daily job lock, so a same-day deploy
	// still repairs leftover active subscription rows.
	if res, recErr := subSvc.ReconcileBillingState(ctx); recErr != nil {
		slog.Error("plan_expiry_job: boot reconcile failed", "error", recErr.Error())
	} else {
		slog.Info("plan_expiry_job: boot reconcile",
			"candidates", res.Candidates,
			"subscription_rows_closed", res.SubscriptionRowsClosed,
			"users_expired", res.UsersExpired,
			"users_marked_past_due", res.UsersMarkedPastDue,
		)
	}
	jobLocks := repository.NewPushJobLockRepository(db)
	job := scheduler.NewPlanExpiryJob(premiumSvc, subSvc, jobLocks)
	if cfg != nil {
		job.AttachAlerter(alert.New(alert.Config{
			Enabled:          cfg.Alert.Enabled,
			WebhookURL:       cfg.Alert.WebhookURL,
			TelegramBotToken: cfg.Alert.TelegramBotToken,
			TelegramChatID:   cfg.Alert.TelegramChatID,
		}))
	}
	job.Start(ctx)
}

// startCheckInReminderJob refreshes D0/D1 flags once per Vietnam civil day.
func startCheckInReminderJob(ctx context.Context, cfg *config.Config, db *gorm.DB) {
	if cfg != nil && !cfg.CheckInReminder.Enabled {
		slog.Info("checkin_reminder_job: disabled via config")
		return
	}
	if db == nil {
		slog.Warn("checkin_reminder_job: skipped — database not available")
		return
	}
	users := repository.NewUserRepository(db)
	checks := repository.NewSkinCheckRepository(db)
	flags := repository.NewCheckInReminderRepository(db)
	vapid := cfg != nil && cfg.HasVAPIDKeys()
	svc := checkinreminderuc.NewService(users, checks, flags, vapid)
	if res, err := svc.RefreshWindow(ctx); err != nil {
		slog.Error("checkin_reminder_job: boot refresh failed", "error", err.Error())
	} else {
		slog.Info("checkin_reminder_job: boot refresh",
			"scanned", res.Scanned,
			"due_d0", res.DueD0,
			"due_d1", res.DueD1,
			"cleared", res.Cleared,
		)
	}
	jobLocks := repository.NewPushJobLockRepository(db)
	scheduler.NewCheckInReminderJob(svc, jobLocks).Start(ctx)
}

// startPendingOrderExpiryJob expires leftover SePay pending orders past the local TTL.
func startPendingOrderExpiryJob(ctx context.Context, cfg *config.Config, db *gorm.DB) {
	if cfg != nil && !cfg.PendingOrderExpiry.Enabled {
		slog.Info("pending_order_expiry_job: disabled via config")
		return
	}
	if db == nil {
		slog.Warn("pending_order_expiry_job: skipped — database not available")
		return
	}
	users := repository.NewUserRepository(db)
	orders := repository.NewPaymentOrderRepository(db)
	logs := repository.NewPlanChangeLogRepository(db)
	paySvc := paymentuc.NewService(db, cfg, orders, users, logs)
	if res, err := paySvc.ExpireStalePending(ctx, time.Time{}); err != nil {
		slog.Error("pending_order_expiry_job: boot expire failed", "error", err.Error())
	} else {
		slog.Info("pending_order_expiry_job: boot expire",
			"expired", res.Expired,
			"pending_fresh", res.PendingFresh,
			"ttl_hours", res.TTLHours,
		)
	}
	jobLocks := repository.NewPushJobLockRepository(db)
	scheduler.NewPendingOrderExpiryJob(paySvc, jobLocks).Start(ctx)
}

// registerUploadServing exposes stored photos under the stable "/uploads/*" path.
//
//   - local driver: serve straight from disk (fast, unchanged dev behavior).
//   - r2 driver:    proxy object bytes from R2 so the public URL shape and the
//     stored DB paths never change (no presigned-URL TTLs leaking to the client).
//
// Always set Access-Control-Allow-Origin on /uploads. Fiber's CORS middleware only
// adds ACAO when the request has an Origin header; CDNs can cache a no-Origin
// response (no ACAO) and later serve it to browser canvas/fetch CORS → fail.
func registerUploadServing(app *fiber.App, store storage.Storage) {
	app.Use("/uploads", func(c *fiber.Ctx) error {
		c.Set("Access-Control-Allow-Origin", "*")
		c.Set("Cross-Origin-Resource-Policy", "cross-origin")
		return c.Next()
	})
	if store.Driver() == "local" {
		app.Static("/uploads", store.LocalDir())
		return
	}
	app.Get("/uploads/*", func(c *fiber.Ctx) error {
		key := c.Params("*")
		if key == "" {
			return fiber.ErrNotFound
		}
		data, err := store.Read(c.UserContext(), key)
		if err != nil {
			return fiber.ErrNotFound
		}
		if ct := mime.TypeByExtension(path.Ext(key)); ct != "" {
			c.Set("Content-Type", ct)
		}
		// Photos are immutable once written (keys are UUIDs), so allow caching.
		// public + ACAO * is safe to CDN-cache (ACAO is constant, not Origin-varying).
		c.Set("Cache-Control", "public, max-age=3600")
		return c.Send(data)
	})
}
