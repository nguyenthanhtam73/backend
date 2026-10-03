// Package mediaurl mints and checks short-lived URLs for user photos.
//
// API responses keep the historical "/uploads/<key>" path and add
// exp + sig query parameters. Web and Android clients prefix the API host
// and do not need to understand the signature.
//
// The HMAC covers the canonical object key, the expiry unix time, and a
// user scope derived from the key (the owning user id, or "public" for
// privacy-blurred share images). A signature minted for one key or scope
// does not authorize a different object.
package mediaurl

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dadiary/backend/internal/storage"
	"github.com/google/uuid"
)

const (
	// DefaultTTL is the lifetime of a signed photo URL when unset.
	DefaultTTL = time.Hour
	// MaxTTL caps configured lifetimes so a typo cannot mint month-long links.
	MaxTTL = 7 * 24 * time.Hour
	// publicScope is the MAC audience for privacy-blurred share images.
	// Those objects are not permanently world-readable: the public share
	// API mints a short-lived URL, and an unsigned request is a 404.
	publicScope = "public"
	deriveInfo  = "dadiary-media-url-v1"
)

// Signer mints and verifies photo URLs.
type Signer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

var defaultSigner atomic.Pointer[Signer]

// SetDefault installs the process-wide signer used when API DTOs are built.
// Tests that change it must restore the previous value.
func SetDefault(s *Signer) { defaultSigner.Store(s) }

// Default returns the process-wide signer, or nil when signing is not configured.
func Default() *Signer { return defaultSigner.Load() }

// New builds a signer.
//
// signingKey is DADIARY_MEDIA_SIGNING_KEY. When it is empty, the key is
// derived from jwtSecret (DADIARY_JWT_SECRET) for development and tests only.
// Production refuses to start without its own media key (see
// config.ValidateStartupSecrets) and must not reach this fallback.
// The JWT secret itself is not used as the HMAC key: the derived key is
// HMAC-SHA256(jwtSecret, "dadiary-media-url-v1").
//
// ttl <= 0 becomes DefaultTTL. ttl above MaxTTL is clamped.
func New(signingKey, jwtSecret string, ttl time.Duration) *Signer {
	material := []byte(strings.TrimSpace(signingKey))
	if len(material) == 0 {
		secret := strings.TrimSpace(jwtSecret)
		if secret == "" {
			return nil
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(deriveInfo))
		material = mac.Sum(nil)
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if ttl > MaxTTL {
		ttl = MaxTTL
	}
	return &Signer{
		secret: material,
		ttl:    ttl,
		now:    time.Now,
	}
}

// TTL returns the configured lifetime.
func (s *Signer) TTL() time.Duration {
	if s == nil || s.ttl <= 0 {
		return DefaultTTL
	}
	return s.ttl
}

// SignedPath returns a relative URL valid for TTL from now.
func (s *Signer) SignedPath(objectKey string) string {
	if s == nil {
		return unsignedPath(objectKey)
	}
	return s.SignedPathAt(objectKey, s.now())
}

// SignedPathAt mints a URL whose expiry is now+TTL. Tests use it to mint
// already-expired links.
func (s *Signer) SignedPathAt(objectKey string, now time.Time) string {
	key, ok := canonicalKey(objectKey)
	if !ok {
		return ""
	}
	if storage.IsPublicAssetKey(key) {
		return "/uploads/" + key
	}
	if s == nil || len(s.secret) == 0 {
		return "/uploads/" + key
	}
	if now.IsZero() {
		now = time.Now()
	}
	exp := now.Add(s.TTL()).Unix()
	sig := s.mac(key, ScopeForKey(key), exp)
	return "/uploads/" + key + "?exp=" + strconv.FormatInt(exp, 10) + "&sig=" + hex.EncodeToString(sig)
}

// Valid reports whether sig is the HMAC for key+exp+scope and exp is still
// in the future. Comparison is constant-time. Malformed, expired, and
// tampered inputs all return false.
func (s *Signer) Valid(objectKey, expRaw, sig string, now time.Time) bool {
	if s == nil || len(s.secret) == 0 {
		return false
	}
	key, ok := canonicalKey(objectKey)
	if !ok || storage.IsPublicAssetKey(key) {
		return false
	}
	exp, err := strconv.ParseInt(strings.TrimSpace(expRaw), 10, 64)
	if err != nil || exp <= 0 {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	if now.Unix() >= exp {
		return false
	}
	want := s.mac(key, ScopeForKey(key), exp)
	sig = strings.ToLower(strings.TrimSpace(sig))
	if len(sig) != hex.EncodedLen(len(want)) {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil || len(got) != len(want) {
		return false
	}
	return hmac.Equal(got, want)
}

func (s *Signer) mac(key, scope string, exp int64) []byte {
	m := hmac.New(sha256.New, s.secret)
	writePart(m, "v1")
	writePart(m, key)
	writePart(m, scope)
	writePart(m, strconv.FormatInt(exp, 10))
	return m.Sum(nil)
}

func writePart(m hash.Hash, part string) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(part)))
	_, _ = m.Write(n[:])
	_, _ = m.Write([]byte(part))
}

// ScopeForKey is the audience bound into the MAC.
//
//   - privacy-blurred share objects (path segment admin-skin-review-public) use "public"
//   - "{username}__{userID}" folders use that user id
//   - legacy "{userID}/..." keys use the leading user id
//   - anything else uses an empty scope (still bound to the exact key)
func ScopeForKey(objectKey string) string {
	key, ok := canonicalKey(objectKey)
	if !ok {
		return ""
	}
	parts := strings.Split(key, "/")
	for _, p := range parts {
		if p == storage.KindAdminSkinReviewPublic {
			return publicScope
		}
	}
	for _, p := range parts {
		if i := strings.LastIndex(p, "__"); i >= 0 {
			id := p[i+2:]
			if _, err := uuid.Parse(id); err == nil {
				return id
			}
		}
	}
	if len(parts) > 0 {
		if _, err := uuid.Parse(parts[0]); err == nil {
			return parts[0]
		}
	}
	return ""
}

// SignClientURL turns a stored upload reference into the URL clients should
// receive. External URLs (Google avatars, etc.) are returned unchanged.
// When no signer is configured, the historical unsigned "/uploads/<key>"
// path is preserved so unit tests that do not boot the API stay stable.
// Production always installs a signer in cmd/api.
func SignClientURL(raw string) string {
	key, ok := ExtractUploadKey(raw)
	if !ok {
		// Stored rows keep a relative object key (userID/file.jpg or the dated
		// PhotoKey). A slash is required so a display name is not treated as a file.
		bare := strings.Trim(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"), "/")
		if bare == "" || strings.Contains(bare, "://") || !strings.Contains(bare, "/") || !storage.SafeObjectKey(bare) {
			return raw
		}
		key = storage.CleanKey(bare)
		if key == "" || !storage.SafeObjectKey(key) {
			return raw
		}
	}
	if storage.IsPublicAssetKey(key) {
		return "/uploads/" + key
	}
	if s := Default(); s != nil {
		return s.SignedPath(key)
	}
	return "/uploads/" + key
}

// SignEach signs every upload reference. Non-upload values pass through.
func SignEach(urls []string) []string {
	if len(urls) == 0 {
		return urls
	}
	out := make([]string, len(urls))
	for i, u := range urls {
		out[i] = SignClientURL(u)
	}
	return out
}

// ExtractUploadKey pulls the object key out of "/uploads/<key>", a full
// https URL with that path, or "uploads/<key>". Query strings are ignored.
func ExtractUploadKey(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	pathPart := raw
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		pathPart = u.Path
	} else {
		pathPart = strings.SplitN(raw, "?", 2)[0]
	}
	pathPart = strings.ReplaceAll(pathPart, "\\", "/")
	const marker = "/uploads/"
	if i := strings.Index(pathPart, marker); i >= 0 {
		pathPart = pathPart[i+len(marker):]
	} else if strings.HasPrefix(pathPart, "uploads/") {
		pathPart = strings.TrimPrefix(pathPart, "uploads/")
	} else {
		return "", false
	}
	return canonicalKey(pathPart)
}

// KeyFromWildcard canonicalizes the Fiber "/uploads/*" capture.
// Encoded and literal traversal is rejected.
func KeyFromWildcard(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.Trim(raw, "/")
	if raw == "" || !storage.SafeObjectKey(raw) {
		return "", false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", false
	}
	decoded = strings.Trim(decoded, "/")
	return canonicalKey(decoded)
}

func canonicalKey(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !storage.SafeObjectKey(raw) {
		return "", false
	}
	key := storage.CleanKey(raw)
	if key == "" || !storage.SafeObjectKey(key) {
		return "", false
	}
	return key, true
}

func unsignedPath(objectKey string) string {
	key, ok := canonicalKey(objectKey)
	if !ok {
		return ""
	}
	return "/uploads/" + key
}
