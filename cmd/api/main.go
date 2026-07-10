package main

import (
	"context"
	"fmt"
	"mime"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/handler"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/storage"
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

	store, err := storage.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "storage: %v\n", err)
		os.Exit(1)
	}
	registerUploadServing(app, store)

	handler.Router(app, cfg, db, tok, store)

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
