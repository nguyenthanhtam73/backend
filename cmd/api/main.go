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

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/handler"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/scheduler"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/dadiary/backend/internal/storage"
	"github.com/dadiary/backend/internal/token"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	pushuc "github.com/dadiary/backend/internal/usecase/push"
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
	startPlanExpiryJob(ctx, db)

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

// startPlanExpiryJob downgrades users whose plan_expires_at has passed (daily UTC).
func startPlanExpiryJob(ctx context.Context, db *gorm.DB) {
	if db == nil {
		slog.Warn("plan_expiry_job: skipped — database not available")
		return
	}
	userRepo := repository.NewUserRepository(db)
	usageRepo := repository.NewUserUsageRepository(db)
	logs := repository.NewPlanChangeLogRepository(db)
	premiumSvc := premiumuc.NewService(userRepo, usageRepo)
	premiumSvc.AttachPlanExpiryDeps(db, userRepo, logs)
	jobLocks := repository.NewPushJobLockRepository(db)
	scheduler.NewPlanExpiryJob(premiumSvc, jobLocks).Start(ctx)
}

// registerUploadServing exposes stored photos under the stable "/uploads/*" path.
//
//   - local driver: serve straight from disk (fast, unchanged dev behavior).
//   - r2 driver:    proxy object bytes from R2 so the public URL shape and the
//     stored DB paths never change (no presigned-URL TTLs leaking to the client).
func registerUploadServing(app *fiber.App, store storage.Storage) {
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
		c.Set("Cache-Control", "private, max-age=3600")
		return c.Send(data)
	})
}
