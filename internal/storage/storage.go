// Package storage abstracts where skin-check / onboarding photos are persisted.
//
// Two drivers are supported:
//   - "local": files on disk under Upload.Dir (dev default; served via app.Static).
//   - "r2":    Cloudflare R2 (S3-compatible), for durable/private production storage.
//
// The stored DB value is always a forward-slash relative *key* (e.g.
// "<userID>/<uuid>.jpg" or "<userID>/onboarding/<uuid>.jpg"). Both drivers use the
// same key, so switching drivers needs no DB migration. Public image URLs stay in
// the "/uploads/<key>" shape regardless of driver — for "r2" the API proxies those
// bytes (see cmd/api), so the frontend never has to change or juggle presigned TTLs.
package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/config"
)

// Storage is the minimal object store used for user photos.
type Storage interface {
	// Save writes data under key. Parent "directories" are created as needed.
	Save(ctx context.Context, key string, data []byte, contentType string) error
	// Read returns the raw bytes stored at key.
	Read(ctx context.Context, key string) ([]byte, error)
	// DeletePrefix removes every object whose key starts with prefix (e.g. a
	// user's whole folder "<userID>/"). Missing prefixes are not an error.
	DeletePrefix(ctx context.Context, prefix string) error
	// Driver reports the active backend: "local" or "r2".
	Driver() string
	// LocalDir returns the absolute on-disk root for the local driver, or "" otherwise.
	LocalDir() string
}

// New constructs a Storage from config. Defaults to the local driver.
func New(cfg *config.Config) (Storage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("storage: nil config")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Storage.Driver)) {
	case "", "local":
		return newLocal(cfg.Upload.Dir)
	case "r2":
		return newR2(cfg.Storage.R2)
	default:
		return nil, fmt.Errorf("storage: unknown driver %q (use local|r2)", cfg.Storage.Driver)
	}
}

// CleanKey normalizes a stored relative path into a canonical storage key:
// backslashes → slashes, no leading slash, no "uploads/" prefix.
func CleanKey(rel string) string {
	k := strings.TrimLeft(strings.ReplaceAll(rel, "\\", "/"), "/")
	k = strings.TrimPrefix(k, "uploads/")
	return k
}
