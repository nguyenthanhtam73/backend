// Package storage abstracts where skin-check / onboarding photos are persisted.
//
// Two drivers are supported:
//   - "local": files on disk under Upload.Dir (dev default; served via app.Static).
//   - "r2":    Cloudflare R2 (S3-compatible), for durable/private production storage.
//
// The stored DB value is always a forward-slash relative *key*. New uploads use
// PhotoKey: "{YYYY}/{MM}/{DD}/{kind}/{username}__{userID}/{uuid}.jpg" so the
// Cloudflare R2 dashboard is browsable by date and user. Legacy keys
// ("{userID}/{uuid}.jpg" / "{userID}/onboarding/...") stay valid — both drivers
// use the same key, so switching drivers needs no DB migration. Public image
// URLs stay in the "/uploads/<key>" shape regardless of driver — for "r2" the
// API proxies those bytes (see cmd/api), so the frontend never has to change
// or juggle presigned TTLs.
package storage

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/dadiary/backend/internal/config"
)

// errUnsafeKey is returned for an empty key, a key containing "..", an
// absolute path, or a prefix that is not scoped to one account.
var errUnsafeKey = errors.New("storage: unsafe key")

// uuidPattern matches the canonical 8-4-4-4-12 form used in photo keys.
var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Storage is the minimal object store used for user photos.
type Storage interface {
	// Save writes data under key. Parent "directories" are created as needed.
	Save(ctx context.Context, key string, data []byte, contentType string) error
	// Read returns the raw bytes stored at key.
	Read(ctx context.Context, key string) ([]byte, error)
	// DeletePrefix removes every object whose key starts with prefix.
	// Used for GDPR wipe: exact keys from the DB, plus the legacy
	// "{userID}/" folder. Missing prefixes are not an error.
	// Empty keys, "..", absolute paths, and (on R2) prefixes that are not
	// scoped to one user are rejected.
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
// It returns "" when the key is empty, contains "..", is absolute, or is
// otherwise unsafe to join onto the uploads root.
func CleanKey(rel string) string {
	k, err := normalizeKey(rel)
	if err != nil {
		return ""
	}
	return k
}

// normalizeKey accepts a relative object key or a public "/uploads/..." URL.
func normalizeKey(rel string) (string, error) {
	raw := strings.TrimSpace(rel)
	if raw == "" {
		return "", errUnsafeKey
	}
	slash := strings.ReplaceAll(raw, "\\", "/")
	// Signed URLs keep the object key in the path and put exp/sig in the query.
	if i := strings.IndexAny(slash, "?#"); i >= 0 {
		slash = slash[:i]
	}
	slash = strings.TrimSpace(slash)
	if slash == "" {
		return "", errUnsafeKey
	}
	if strings.HasPrefix(slash, "//") || isWindowsDrive(slash) {
		return "", errUnsafeKey
	}
	// A leading slash is absolute. The only exception is the public URL
	// prefix the API already stores and serves.
	if strings.HasPrefix(slash, "/") && !strings.HasPrefix(slash, "/uploads/") {
		return "", errUnsafeKey
	}
	k := strings.TrimPrefix(strings.TrimLeft(slash, "/"), "uploads/")
	k = strings.Trim(k, "/")
	if k == "" || strings.Contains(k, "..") {
		return "", errUnsafeKey
	}
	for _, part := range strings.Split(k, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errUnsafeKey
		}
	}
	return k, nil
}

// userScopedPrefix is the R2/S3 delete prefix. A short or unscoped prefix
// would list other accounts' objects, so it must be a legacy "{uuid}/..."
// key or a PhotoKey folder that contains "__{uuid}".
func userScopedPrefix(prefix string) (string, error) {
	k, err := normalizeKey(prefix)
	if err != nil {
		return "", err
	}
	if len(k) < 36 {
		return "", fmt.Errorf("%w: prefix too short", errUnsafeKey)
	}
	if !isUserScopedKey(k) {
		return "", fmt.Errorf("%w: prefix is not user-scoped", errUnsafeKey)
	}
	parts := strings.Split(k, "/")
	if len(parts) == 1 && uuidPattern.MatchString(parts[0]) {
		return parts[0] + "/", nil
	}
	return k, nil
}

func isUserScopedKey(k string) bool {
	parts := strings.Split(k, "/")
	if uuidPattern.MatchString(parts[0]) {
		return true
	}
	for _, part := range parts {
		i := strings.LastIndex(part, "__")
		if i < 0 {
			continue
		}
		if uuidPattern.MatchString(part[i+2:]) {
			return true
		}
	}
	return false
}

func isWindowsDrive(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[0]
	letter := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	return letter && s[1] == ':'
}
