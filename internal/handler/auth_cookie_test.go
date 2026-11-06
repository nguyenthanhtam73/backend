package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/lobby"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/gofiber/fiber/v2"
)

func TestAuthSetsHttpOnlyAccessCookie(t *testing.T) {
	app := newClientAuthApp(t)

	status, res, payload := doJSONRes(t, app, http.MethodPost, "/auth/register",
		`{"email":"cookie@example.com","password":"password1"}`, "", nil)
	if status != http.StatusCreated {
		t.Fatalf("register %d %+v", status, payload)
	}
	access, refresh := tokensFrom(t, payload)
	cookie := mustAccessCookie(t, res)
	assertAccessCookie(t, cookie, access, time.Now().UTC().Add(24*time.Hour))
	if cookie.Value == refresh || strings.Contains(cookieHeader(res), refresh) {
		t.Fatal("cookie stored the refresh token")
	}

	issued := time.Now().UTC()
	status, loginRes, login := doJSONRes(t, app, http.MethodPost, "/auth/login",
		`{"email":"cookie@example.com","password":"password1"}`, "", nil)
	if status != http.StatusOK {
		t.Fatalf("login %d %+v", status, login)
	}
	loginAccess, _ := tokensFrom(t, login)
	if _, ok := login["data"].(map[string]any)["tokens"].(map[string]any)["access_token"].(string); !ok {
		t.Fatal("login dropped the bearer token body")
	}
	assertAccessCookie(t, mustAccessCookie(t, loginRes), loginAccess, issued.Add(24*time.Hour))

	status, badRes, bad := doJSONRes(t, app, http.MethodPost, "/auth/login",
		`{"email":"cookie@example.com","password":"wrong-pass"}`, "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("bad login %d %+v", status, bad)
	}
	if cookieHeader(badRes) != "" {
		t.Fatalf("failed login set a cookie")
	}

	status, refreshRes, refreshed := doJSONRes(t, app, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+refresh+`"}`, "", nil)
	if status != http.StatusOK {
		t.Fatalf("refresh %d %+v", status, refreshed)
	}
	refreshedAccess, refreshedRefresh := tokensFrom(t, refreshed)
	refCookie := mustAccessCookie(t, refreshRes)
	assertAccessCookie(t, refCookie, refreshedAccess, time.Now().UTC().Add(24*time.Hour))
	if refCookie.Value == refreshedRefresh {
		t.Fatal("refresh cookie stored the refresh token")
	}

	status, sessionRes, session := doJSONRes(t, app, http.MethodPost, "/auth/session-cookie", "", loginAccess, nil)
	if status != http.StatusOK {
		t.Fatalf("session-cookie %d %+v", status, session)
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), loginAccess) || strings.Contains(string(encoded), "cookie@example.com") {
		t.Fatal("session-cookie response echoed a secret")
	}
	assertAccessCookie(t, mustAccessCookie(t, sessionRes), loginAccess, issued.Add(24*time.Hour))

	status, logoutRes, logout := doJSONRes(t, app, http.MethodPost, "/auth/logout", "", loginAccess, nil)
	if status != http.StatusOK {
		t.Fatalf("logout %d %+v", status, logout)
	}
	cleared := mustAccessCookie(t, logoutRes)
	assertCookieFlags(t, cleared)
	if !cookieCleared(cleared) {
		t.Fatalf("logout did not clear the cookie: %s", cookieHeader(logoutRes))
	}

	status, _, again := doJSONRes(t, app, http.MethodPost, "/auth/login",
		`{"email":"cookie@example.com","password":"password1"}`, "", nil)
	if status != http.StatusOK {
		t.Fatalf("relogin %d %+v", status, again)
	}
	access2, _ := tokensFrom(t, again)
	status, allRes, all := doJSONRes(t, app, http.MethodPost, "/auth/logout-all", "", access2, nil)
	if status != http.StatusOK {
		t.Fatalf("logout-all %d %+v", status, all)
	}
	cleared = mustAccessCookie(t, allRes)
	assertCookieFlags(t, cleared)
	if !cookieCleared(cleared) {
		t.Fatalf("logout-all did not clear the cookie: %s", cookieHeader(allRes))
	}
	// The access JWT is still a Bearer credential after the cookie is cleared.
	meStatus, me := doJSON(t, app, http.MethodGet, "/me", "", access2, nil)
	if meStatus != http.StatusOK {
		t.Fatalf("bearer /me after cookie clear %d %+v", meStatus, me)
	}
}

func TestSessionCookieRequiresBearer(t *testing.T) {
	app := newClientAuthApp(t)
	status, res, payload := doJSONRes(t, app, http.MethodPost, "/auth/session-cookie", "", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("status %d %+v", status, payload)
	}
	if cookieHeader(res) != "" {
		t.Fatal("unauthenticated session-cookie set a cookie")
	}
}

func TestWebOriginCanStoreAuthCookie(t *testing.T) {
	app := fiber.New()
	middleware.RegisterDefault(app)
	app.Post("/api/v1/auth/login", func(c *fiber.Ctx) error {
		setAccessCookie(c, "header.payload.sig", time.Now().UTC().Add(time.Hour))
		return c.JSON(fiber.Map{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://dadiary.vn")
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "https://dadiary.vn" {
		t.Fatalf("ACAO %q", got)
	}
	if res.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("credentials %q", res.Header.Get("Access-Control-Allow-Credentials"))
	}
	if len(res.Header.Values("Access-Control-Allow-Origin")) != 1 {
		t.Fatalf("duplicate ACAO %v", res.Header.Values("Access-Control-Allow-Origin"))
	}
	assertCookieFlags(t, mustAccessCookie(t, res))

	pre := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	pre.Header.Set("Origin", "https://www.dadiary.vn")
	pre.Header.Set("Access-Control-Request-Method", "POST")
	pre.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	preRes, err := app.Test(pre, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer preRes.Body.Close()
	if preRes.StatusCode >= 400 {
		t.Fatalf("preflight %d", preRes.StatusCode)
	}
	if preRes.Header.Get("Access-Control-Allow-Origin") != "https://www.dadiary.vn" || preRes.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("www preflight ACAO %q credentials %q", preRes.Header.Get("Access-Control-Allow-Origin"), preRes.Header.Get("Access-Control-Allow-Credentials"))
	}
	allowHeaders := strings.ToLower(preRes.Header.Get("Access-Control-Allow-Headers"))
	if !strings.Contains(allowHeaders, "authorization") || !strings.Contains(allowHeaders, "x-dadiary-client") {
		t.Fatalf("allow-headers %q", preRes.Header.Get("Access-Control-Allow-Headers"))
	}

	other := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	other.Header.Set("Origin", "https://evil.example")
	other.Header.Set("Content-Type", "application/json")
	otherRes, err := app.Test(other, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer otherRes.Body.Close()
	if otherRes.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("other origin ACAO %q", otherRes.Header.Get("Access-Control-Allow-Origin"))
	}
	if strings.EqualFold(otherRes.Header.Get("Access-Control-Allow-Credentials"), "true") {
		t.Fatal("foreign origin received credentialed CORS")
	}

	wild := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	wild.Header.Set("Origin", "https://example.com")
	wild.Header.Set("Access-Control-Request-Method", "GET")
	wild.Header.Set("Access-Control-Request-Headers", "x-dadiary-client,authorization")
	wildRes, err := app.Test(wild, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer wildRes.Body.Close()
	wildHeaders := strings.ToLower(wildRes.Header.Get("Access-Control-Allow-Headers"))
	if !strings.Contains(wildHeaders, "x-dadiary-client") {
		t.Fatalf("wildcard allow-headers %q", wildRes.Header.Get("Access-Control-Allow-Headers"))
	}
}

func doJSONRes(t *testing.T, app *fiber.App, method, path, body, auth string, headers map[string]string) (int, *http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("status %d body %s: %v", res.StatusCode, raw, err)
		}
	}
	return res.StatusCode, res, payload
}

func cookieHeader(res *http.Response) string {
	if res == nil {
		return ""
	}
	return strings.Join(res.Header.Values("Set-Cookie"), "\n")
}

func mustAccessCookie(t *testing.T, res *http.Response) *http.Cookie {
	t.Helper()
	if res == nil {
		t.Fatal("nil response")
	}
	for _, c := range res.Cookies() {
		if c.Name == lobby.AccessCookie {
			return c
		}
	}
	t.Fatalf("missing %s", lobby.AccessCookie)
	return nil
}

func assertAccessCookie(t *testing.T, cookie *http.Cookie, access string, expiry time.Time) {
	t.Helper()
	if cookie.Value != access {
		t.Fatal("cookie value is not the access token")
	}
	assertCookieFlags(t, cookie)
	// fasthttp writes Max-Age and omits Expires when both are set.
	// Browsers honor Max-Age, so that is the access-token lifetime.
	want := int(time.Until(expiry).Seconds())
	if cookie.MaxAge < want-30 || cookie.MaxAge > want+30 {
		t.Fatalf("max-age %d want about %d", cookie.MaxAge, want)
	}
}

func assertCookieFlags(t *testing.T, cookie *http.Cookie) {
	t.Helper()
	if !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("flags httponly %v secure %v", cookie.HttpOnly, cookie.Secure)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("samesite %v", cookie.SameSite)
	}
	if cookie.Domain != accessCookieDomain || cookie.Path != "/" {
		t.Fatalf("domain %q path %q", cookie.Domain, cookie.Path)
	}
}

func cookieCleared(cookie *http.Cookie) bool {
	if cookie == nil || cookie.Value != "" {
		return false
	}
	if cookie.MaxAge < 0 {
		return true
	}
	return !cookie.Expires.IsZero() && cookie.Expires.Before(time.Now().Add(-time.Hour))
}
