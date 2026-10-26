package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/mediaurl"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/storage"
	"github.com/gofiber/fiber/v2"
)

// TestUploads_PlainLegacyURLReturns404 is the old public contract: a bare
// /uploads/<key> with no exp/sig used to serve the file. It must now 404
// even though the object is still on disk (a signed URL for the same key works).
func TestUploads_PlainLegacyURLReturns404(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.New(&config.Config{Upload: config.UploadConfig{Dir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	// Production key shape: date / kind / username__userID / uuid.ext
	key := "2026/08/29/check-in/tam-nguyen__550e8400-e29b-41d4-a716-446655440000/6f1e2d3c-4b5a-6789-abcd-ef0123456789.jpg"
	if err := store.Save(context.Background(), key, []byte("face"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	signer := mediaurl.New("test-media-key", "", time.Hour)
	app := fiber.New()
	RegisterUploads(app, store, signer)

	plain := "/uploads/" + key
	res, body := do(t, app, http.MethodGet, plain, "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("plain legacy url status=%d body=%q", res.StatusCode, body)
	}
	if strings.Contains(string(body), "face") {
		t.Fatalf("plain url returned bytes: %q", body)
	}

	res, body = do(t, app, http.MethodGet, signer.SignedPath(key), "")
	if res.StatusCode != http.StatusOK || string(body) != "face" {
		t.Fatalf("signed same key status=%d body=%q", res.StatusCode, body)
	}
}

func TestUploads_SignedAccess(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.New(&config.Config{Upload: config.UploadConfig{Dir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	userA := "11111111-1111-1111-1111-111111111111"
	userB := "22222222-2222-2222-2222-222222222222"
	keyA := userA + "/a.jpg"
	keyB := userB + "/b.jpg"
	ctx := context.Background()
	if err := store.Save(ctx, keyA, []byte("photo-a"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, keyB, []byte("photo-b"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "brand/logo.png", []byte("logo"), "image/png"); err != nil {
		t.Fatal(err)
	}

	signer := mediaurl.New("test-media-key", "", time.Hour)
	app := fiber.New()
	middleware.RegisterDefault(app)
	RegisterUploads(app, store, signer)

	valid := signer.SignedPath(keyA)
	res, body := do(t, app, http.MethodGet, valid, "https://dadiary.vn")
	if res.StatusCode != http.StatusOK || string(body) != "photo-a" {
		t.Fatalf("valid status=%d body=%q", res.StatusCode, body)
	}
	if cc := res.Header.Get("Cache-Control"); !strings.HasPrefix(cc, "private, max-age=") {
		t.Fatalf("cache-control=%q", cc)
	}
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "https://dadiary.vn" {
		t.Fatalf("acao=%q", got)
	}
	if got := res.Header.Get("Access-Control-Allow-Origin"); got == "*" {
		t.Fatal("wildcard CORS on a user photo")
	}

	res, body = do(t, app, http.MethodGet, "/uploads/"+keyA, "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("unsigned status=%d body=%q", res.StatusCode, body)
	}
	if strings.Contains(string(body), "photo-a") || strings.Contains(strings.ToLower(string(body)), "forbidden") {
		t.Fatalf("unsigned body leaked file or existence: %q", body)
	}

	expired := signer.SignedPathAt(keyA, time.Now().Add(-2*time.Hour))
	res, _ = do(t, app, http.MethodGet, expired, "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expired status=%d", res.StatusCode)
	}

	tampered := valid[:len(valid)-1] + flip(valid[len(valid)-1])
	res, _ = do(t, app, http.MethodGet, tampered, "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("tampered status=%d", res.StatusCode)
	}

	u, err := url.Parse(valid)
	if err != nil {
		t.Fatal(err)
	}
	forged := "/uploads/" + keyB + "?" + u.RawQuery
	res, body = do(t, app, http.MethodGet, forged, "")
	if res.StatusCode != http.StatusNotFound || strings.Contains(string(body), "photo-b") {
		t.Fatalf("cross-user status=%d body=%q", res.StatusCode, body)
	}

	for _, p := range []string{
		"/uploads/../secret",
		"/uploads/%2e%2e/secret",
		"/uploads/" + keyA + "/../../" + keyB + "?" + u.RawQuery,
	} {
		res, body = do(t, app, http.MethodGet, p, "")
		if res.StatusCode != http.StatusNotFound || strings.Contains(string(body), "photo-") {
			t.Fatalf("traversal %s status=%d body=%q", p, res.StatusCode, body)
		}
	}

	res, body = do(t, app, http.MethodGet, "/uploads/brand/logo.png", "")
	if res.StatusCode != http.StatusOK || string(body) != "logo" {
		t.Fatalf("brand status=%d body=%q", res.StatusCode, body)
	}
	if cc := res.Header.Get("Cache-Control"); !strings.HasPrefix(cc, "public,") {
		t.Fatalf("brand cache-control=%q", cc)
	}
}

func do(t *testing.T, app *fiber.App, method, target, origin string) (*http.Response, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return res, body
}

func TestUploads_MissingSigningKeyAndJWTFailClosed(t *testing.T) {
	key := "11111111-1111-1111-1111-111111111111/a.jpg"
	signer := mediaurl.New("", "", time.Hour)
	if signer != nil {
		t.Fatal("empty signing key and empty JWT secret produced a signer")
	}
	if got := signer.SignedPath(key); got != "" {
		t.Fatalf("issued a url with no key: %q", got)
	}
	if signer.Valid(key, "9999999999", "00", time.Now()) {
		t.Fatal("empty keys accepted a signature")
	}

	mediaurl.SetDefault(nil)
	t.Cleanup(func() { mediaurl.SetDefault(nil) })
	issued := mediaurl.SignClientURL("/uploads/" + key)
	if strings.Contains(issued, "sig=") || strings.Contains(issued, "exp=") {
		t.Fatalf("no signer minted a signature: %s", issued)
	}

	dir := t.TempDir()
	store, err := storage.New(&config.Config{Upload: config.UploadConfig{Dir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), key, []byte("secret-face"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	RegisterUploads(app, store, nil)

	for _, target := range []string{
		"/uploads/" + key,
		issued,
		"/uploads/" + key + "?exp=9999999999&sig=00",
	} {
		res, body := do(t, app, http.MethodGet, target, "")
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status=%d body=%q", target, res.StatusCode, body)
		}
		if strings.Contains(string(body), "secret-face") {
			t.Fatalf("%s served the photo: %q", target, body)
		}
	}
}

func TestUploads_SignedURLServesWithoutAuthorization(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.New(&config.Config{Upload: config.UploadConfig{Dir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	key := "11111111-1111-1111-1111-111111111111/a.jpg"
	if err := store.Save(context.Background(), key, []byte("photo-a"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	signer := mediaurl.New("test-media-key-at-least-32-bytes!!", "", time.Hour)
	app := fiber.New()
	RegisterUploads(app, store, signer)

	signed := signer.SignedPath(key)
	req := httptest.NewRequest(http.MethodGet, signed, nil)
	if req.Header.Get("Authorization") != "" {
		t.Fatal("test request must not send Authorization")
	}
	res, body := do(t, app, http.MethodGet, signed, "")
	if res.StatusCode != http.StatusOK || string(body) != "photo-a" {
		t.Fatalf("signed without authorization status=%d body=%q", res.StatusCode, body)
	}

	res, body = do(t, app, http.MethodGet, "/uploads/"+key, "")
	if res.StatusCode != http.StatusNotFound || strings.Contains(string(body), "photo-a") {
		t.Fatalf("unsigned status=%d body=%q", res.StatusCode, body)
	}

	expired := signer.SignedPathAt(key, time.Now().Add(-2*time.Hour))
	res, body = do(t, app, http.MethodGet, expired, "")
	if res.StatusCode != http.StatusNotFound || strings.Contains(string(body), "photo-a") {
		t.Fatalf("expired status=%d body=%q", res.StatusCode, body)
	}

	tampered := signed[:len(signed)-1] + flip(signed[len(signed)-1])
	res, body = do(t, app, http.MethodGet, tampered, "")
	if res.StatusCode != http.StatusNotFound || strings.Contains(string(body), "photo-a") {
		t.Fatalf("tampered status=%d body=%q", res.StatusCode, body)
	}
}

func TestUploads_JWTSecretFallbackSigns(t *testing.T) {
	const jwtSecret = "jwt-secret-at-least-32-bytes-long!!"
	derived := mediaurl.New("", jwtSecret, time.Hour)
	if derived == nil {
		t.Fatal("expected a signer derived from the JWT secret")
	}
	raw := mediaurl.New(jwtSecret, "", time.Hour)
	key := "11111111-1111-1111-1111-111111111111/a.jpg"
	signed := derived.SignedPath(key)
	if signed == "" || !strings.Contains(signed, "sig=") {
		t.Fatalf("fallback issued %q", signed)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Valid(key, u.Query().Get("exp"), u.Query().Get("sig"), time.Now()) {
		t.Fatal("fallback HMAC key equals the raw JWT secret")
	}
	if !derived.Valid(key, u.Query().Get("exp"), u.Query().Get("sig"), time.Now()) {
		t.Fatal("derived signer rejected its own url")
	}

	dir := t.TempDir()
	store, err := storage.New(&config.Config{Upload: config.UploadConfig{Dir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), key, []byte("photo-a"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	RegisterUploads(app, store, derived)

	res, body := do(t, app, http.MethodGet, "/uploads/"+key, "")
	if res.StatusCode != http.StatusNotFound || strings.Contains(string(body), "photo-a") {
		t.Fatalf("unsigned status=%d body=%q", res.StatusCode, body)
	}
	res, body = do(t, app, http.MethodGet, signed, "")
	if res.StatusCode != http.StatusOK || string(body) != "photo-a" {
		t.Fatalf("signed without jwt status=%d body=%q", res.StatusCode, body)
	}
}

func flip(b byte) string {
	if b == '0' {
		return "1"
	}
	return "0"
}
