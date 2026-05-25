package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/handler"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/token"
	"github.com/gofiber/fiber/v2"
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

	if abs, err := filepath.Abs(cfg.Upload.Dir); err == nil {
		if mkErr := os.MkdirAll(abs, 0o755); mkErr != nil {
			fmt.Fprintf(os.Stderr, "upload dir: %v\n", mkErr)
		} else {
			app.Static("/uploads", abs)
		}
	}

	handler.Router(app, cfg, db, tok)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
