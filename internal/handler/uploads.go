package handler

import (
	"fmt"
	"mime"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/mediaurl"
	"github.com/dadiary/backend/internal/storage"
	"github.com/gofiber/fiber/v2"
)

// RegisterUploads serves stored photos at /uploads/*.
//
// User photos require a valid unexpired signature. A missing, expired, or
// tampered signature is a 404 — the same response as a missing object — so
// the route does not confirm that a file exists. Non-user assets under
// brand/ and static/ stay public.
func RegisterUploads(app *fiber.App, store storage.Storage, signer *mediaurl.Signer) {
	h := func(c *fiber.Ctx) error {
		return serveUpload(c, store, signer)
	}
	app.Get("/uploads/*", h)
	app.Head("/uploads/*", h)
}

func serveUpload(c *fiber.Ctx, store storage.Storage, signer *mediaurl.Signer) error {
	if store == nil {
		return uploadNotFound(c)
	}
	key, ok := mediaurl.KeyFromWildcard(c.Params("*"))
	if !ok {
		return uploadNotFound(c)
	}
	if storage.IsPublicAssetKey(key) {
		return sendUpload(c, store, key, true, 3600)
	}
	expRaw := c.Query("exp")
	sig := c.Query("sig")
	if signer == nil || !signer.Valid(key, expRaw, sig, time.Now()) {
		return uploadNotFound(c)
	}
	exp, _ := strconv.ParseInt(expRaw, 10, 64)
	remaining := exp - time.Now().Unix()
	if remaining < 0 {
		remaining = 0
	}
	return sendUpload(c, store, key, false, int(remaining))
}

func uploadNotFound(c *fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "private, no-store")
	return fiber.ErrNotFound
}

func sendUpload(c *fiber.Ctx, store storage.Storage, key string, publicAsset bool, maxAge int) error {
	data, err := store.Read(c.UserContext(), key)
	if err != nil || len(data) == 0 {
		return uploadNotFound(c)
	}
	if ct := mime.TypeByExtension(path.Ext(key)); ct != "" {
		c.Set(fiber.HeaderContentType, ct)
	}
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("Cross-Origin-Resource-Policy", "cross-origin")
	if publicAsset {
		// Constant ACAO. These keys are not user photos, so a shared cache
		// may store the response. User photos never take this branch.
		c.Set(fiber.HeaderCacheControl, "public, max-age=3600")
		c.Set(fiber.HeaderAccessControlAllowOrigin, "*")
		return c.Send(data)
	}
	if maxAge < 0 {
		maxAge = 0
	}
	// private: shared caches (CDNs) must not store the bytes. Echo Origin
	// instead of "*" so a cached response is not a wildcard CORS grant.
	c.Set(fiber.HeaderCacheControl, fmt.Sprintf("private, max-age=%d", maxAge))
	c.Response().Header.Del(fiber.HeaderAccessControlAllowOrigin)
	if origin := safeRequestOrigin(c.Get("Origin")); origin != "" {
		c.Set(fiber.HeaderAccessControlAllowOrigin, origin)
		c.Set(fiber.HeaderVary, "Origin")
	}
	return c.Send(data)
}

func safeRequestOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	switch u.Scheme {
	case "https", "http":
	default:
		return ""
	}
	return u.Scheme + "://" + u.Host
}
