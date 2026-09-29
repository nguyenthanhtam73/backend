package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/token"
	funneleventuc "github.com/dadiary/backend/internal/usecase/funnelevent"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openFunnelDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:funnel_events_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.FunnelEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func newFunnelFixture(t *testing.T, withCORS bool) (*fiber.App, *gorm.DB, *token.Service) {
	t.Helper()
	db := openFunnelDB(t)
	tok, err := token.NewService(config.JWTConfig{
		Secret:     "test-secret-for-funnel-events-32b",
		AccessTTL:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	if withCORS {
		middleware.RegisterDefault(app)
	}
	Router(app, &config.Config{}, db, tok, nil)
	return app, db, tok
}

func funnelBody(event, sessionID, path, props string) string {
	if props == "" {
		props = `{}`
	}
	return fmt.Sprintf(
		`{"event":%q,"session_id":%q,"path":%q,"props":%s,"client_ts":"2026-09-29T10:43:00.123Z"}`,
		event, sessionID, path, props,
	)
}

func postFunnel(t *testing.T, app *fiber.App, auth, ua, origin, body string) (int, []byte, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/funnel-events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
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
	return res.StatusCode, raw, res.Header
}

func errorFields(t *testing.T, raw []byte) (string, string) {
	t.Helper()
	var env struct {
		Success bool `json:"success"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if env.Success {
		t.Fatalf("expected error envelope, got %s", raw)
	}
	return env.Error.Code, env.Error.Message
}

func TestFunnelEvents_AcceptsWithAndWithoutAuth(t *testing.T) {
	app, db, tok := newFunnelFixture(t, true)
	uid := uuid.New()
	access, err := tok.SignAccess(uid)
	if err != nil {
		t.Fatal(err)
	}

	guestBody := funnelBody(domain.FunnelCheckinPageView, "guest-sess", "/check-in", `{"has_photo":false}`)
	status, raw, hdr := postFunnel(t, app, "", "DaDiaryTest/1", "https://dadiary.vn", guestBody)
	if status != http.StatusNoContent {
		t.Fatalf("guest status=%d body=%s", status, raw)
	}
	if len(raw) != 0 {
		t.Fatalf("guest body=%q, want empty 204", raw)
	}
	acao := hdr.Get("Access-Control-Allow-Origin")
	if acao != "*" && acao != "https://dadiary.vn" {
		t.Fatalf("Access-Control-Allow-Origin=%q", acao)
	}

	preflight := httptest.NewRequest(http.MethodOptions, "/api/v1/funnel-events", nil)
	preflight.Header.Set("Origin", "https://dadiary.vn")
	preflight.Header.Set("Access-Control-Request-Method", "POST")
	preflight.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	preRes, err := app.Test(preflight, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer preRes.Body.Close()
	if preRes.StatusCode >= 400 {
		t.Fatalf("preflight status=%d", preRes.StatusCode)
	}
	if got := preRes.Header.Get("Access-Control-Allow-Origin"); got != "*" && got != "https://dadiary.vn" {
		t.Fatalf("preflight ACAO=%q", got)
	}
	if allow := preRes.Header.Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(allow), "authorization") {
		t.Fatalf("preflight allow-headers=%q", allow)
	}

	badBody := funnelBody(domain.FunnelCheckinFormView, "bad-token-sess", "/check-in", `{}`)
	status, raw, _ = postFunnel(t, app, "Bearer not-a-jwt", "", "", badBody)
	if status != http.StatusNoContent {
		t.Fatalf("invalid token status=%d body=%s", status, raw)
	}

	longUA := strings.Repeat("A", 300)
	authedBody := funnelBody(domain.FunnelCheckinSubmitSuccess, "user-sess", "/check-in", `{"photo_count":1,"ok":true}`)
	status, raw, _ = postFunnel(t, app, "Bearer "+access, longUA, "", authedBody)
	if status != http.StatusNoContent {
		t.Fatalf("authed status=%d body=%s", status, raw)
	}

	seen := map[string]bool{
		domain.FunnelCheckinPageView:      true,
		domain.FunnelCheckinFormView:      true,
		domain.FunnelCheckinSubmitSuccess: true,
	}
	for _, event := range domain.AllFunnelEvents {
		if seen[event] {
			continue
		}
		body := funnelBody(event, "wl-"+event, "/check-in", `{"n":1}`)
		status, raw, _ = postFunnel(t, app, "", "", "", body)
		if status != http.StatusNoContent {
			t.Fatalf("event %s status=%d body=%s", event, status, raw)
		}
	}

	var guest domain.FunnelEvent
	if err := db.Where("session_id = ?", "guest-sess").First(&guest).Error; err != nil {
		t.Fatal(err)
	}
	if guest.UserID != nil {
		t.Fatalf("guest user_id=%v", guest.UserID)
	}
	if guest.Event != domain.FunnelCheckinPageView || guest.Path != "/check-in" {
		t.Fatalf("guest row=%+v", guest)
	}
	if guest.UserAgent != "DaDiaryTest/1" {
		t.Fatalf("guest user_agent=%q", guest.UserAgent)
	}
	assertClientTS(t, guest.ClientTS)
	if guest.ServerTS.IsZero() || time.Since(guest.ServerTS) > time.Minute {
		t.Fatalf("guest server_ts=%s", guest.ServerTS)
	}
	assertProps(t, guest.Props, map[string]any{"has_photo": false})

	var bad domain.FunnelEvent
	if err := db.Where("session_id = ?", "bad-token-sess").First(&bad).Error; err != nil {
		t.Fatal(err)
	}
	if bad.UserID != nil {
		t.Fatalf("invalid token stored user_id=%v", bad.UserID)
	}

	var authed domain.FunnelEvent
	if err := db.Where("session_id = ?", "user-sess").First(&authed).Error; err != nil {
		t.Fatal(err)
	}
	if authed.UserID == nil || *authed.UserID != uid {
		t.Fatalf("authed user_id=%v want %s", authed.UserID, uid)
	}
	if authed.UserAgent != strings.Repeat("A", domain.MaxFunnelUserAgentRunes) {
		t.Fatalf("user_agent len=%d", len(authed.UserAgent))
	}
	assertProps(t, authed.Props, map[string]any{"photo_count": float64(1), "ok": true})

	type pragmaCol struct {
		Name string `gorm:"column:name"`
	}
	var cols []pragmaCol
	if err := db.Raw("PRAGMA table_info(funnel_events)").Scan(&cols).Error; err != nil {
		t.Fatal(err)
	}
	if len(cols) == 0 {
		t.Fatal("funnel_events has no columns")
	}
	for _, col := range cols {
		name := strings.ToLower(col.Name)
		if strings.Contains(name, "ip") || strings.Contains(name, "email") {
			t.Fatalf("funnel_events stores %s", col.Name)
		}
	}
	type indexName struct {
		Name string `gorm:"column:name"`
	}
	var indexes []indexName
	if err := db.Raw("SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'funnel_events'").Scan(&indexes).Error; err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, idx := range indexes {
		found[idx.Name] = true
	}
	for _, name := range []string{"idx_funnel_events_user_server", "idx_funnel_events_event_server"} {
		if !found[name] {
			t.Fatalf("missing index %s", name)
		}
	}
}

func TestFunnelEvents_RejectsUnknownEventAndOversizedProps(t *testing.T) {
	app, db, _ := newFunnelFixture(t, false)
	cases := []struct {
		name    string
		body    string
		code    string
		message string
	}{
		{
			name:    "unknown event",
			body:    funnelBody("purchase", "sess-unknown", "/check-in", `{}`),
			code:    "invalid_funnel_event",
			message: "unknown event",
		},
		{
			name:    "oversized props",
			body:    funnelBody(domain.FunnelCheckinPageView, "sess-big", "/check-in", `{"note":"`+strings.Repeat("x", 3000)+`"}`),
			code:    "invalid_funnel_event",
			message: "props is too large",
		},
		{
			name:    "nested props",
			body:    funnelBody(domain.FunnelCheckinPageView, "sess-nested", "/check-in", `{"meta":{"a":1}}`),
			code:    "invalid_funnel_event",
			message: "props must be a flat object",
		},
		{
			name:    "session id too long",
			body:    funnelBody(domain.FunnelCheckinPageView, strings.Repeat("s", 65), "/check-in", `{}`),
			code:    "invalid_funnel_event",
			message: "session_id is too long",
		},
		{
			name:    "path too long",
			body:    funnelBody(domain.FunnelCheckinPageView, "sess-path", "/"+strings.Repeat("p", 200), `{}`),
			code:    "invalid_funnel_event",
			message: "path is too long",
		},
		{
			name:    "body too large",
			body:    funnelBody(domain.FunnelCheckinPageView, "sess-body", "/check-in", `{"note":"`+strings.Repeat("x", 9000)+`"}`),
			code:    "invalid_funnel_event",
			message: "body is too large",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw, _ := postFunnel(t, app, "", "", "", tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", status, raw)
			}
			code, msg := errorFields(t, raw)
			if code != tc.code || msg != tc.message {
				t.Fatalf("error=%s %q", code, msg)
			}
		})
	}
	var n int64
	if err := db.Model(&domain.FunnelEvent{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rejected events were stored: %d", n)
	}
}

func TestFunnelEvents_RateLimit(t *testing.T) {
	if funnelEventRateMax != 60 || funnelEventGlobalRateMax != 600 || funnelEventRateWindow != time.Minute {
		t.Fatalf("production budget = %d/session and %d global per %s", funnelEventRateMax, funnelEventGlobalRateMax, funnelEventRateWindow)
	}
	app, db, _ := newFunnelFixture(t, false)
	body := funnelBody(domain.FunnelCheckinPageView, "sess-rate", "/check-in", `{"n":1}`)
	for i := 0; i < funnelEventRateMax; i++ {
		status, raw, _ := postFunnel(t, app, "", "", "", body)
		if status != http.StatusNoContent {
			t.Fatalf("request %d status=%d body=%s", i+1, status, raw)
		}
	}
	status, raw, _ := postFunnel(t, app, "", "", "", body)
	if status != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	code, _ := errorFields(t, raw)
	if code != "rate_limited" {
		t.Fatalf("code=%s", code)
	}
	// A second session_id is a different bucket. c.IP() is not the key,
	// so visitors who share Railway's proxy address do not share this cap.
	rotated := funnelBody(domain.FunnelCheckinPageView, "sess-rotated", "/check-in", `{"n":1}`)
	status, raw, _ = postFunnel(t, app, "", "", "", rotated)
	if status != http.StatusNoContent {
		t.Fatalf("rotated session status=%d body=%s", status, raw)
	}
	var n int64
	if err := db.Model(&domain.FunnelEvent{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != int64(funnelEventRateMax+1) {
		t.Fatalf("rows=%d want %d", n, funnelEventRateMax+1)
	}
}

func TestFunnelEvents_GlobalRateLimit(t *testing.T) {
	db := openFunnelDB(t)
	h := NewFunnelEventHandler(funneleventuc.NewService(repository.NewFunnelEventRepository(db)))
	app := fiber.New()
	app.Post("/api/v1/funnel-events", middleware.FunnelEventGlobalLimiter(2, time.Minute), h.Log)

	for i, sid := range []string{"g-a", "g-b"} {
		body := funnelBody(domain.FunnelCheckinPageView, sid, "/check-in", `{}`)
		status, raw, _ := postFunnel(t, app, "", "", "", body)
		if status != http.StatusNoContent {
			t.Fatalf("session %d status=%d body=%s", i+1, status, raw)
		}
	}
	body := funnelBody(domain.FunnelCheckinPageView, "g-c", "/check-in", `{}`)
	status, raw, _ := postFunnel(t, app, "", "", "", body)
	if status != http.StatusTooManyRequests {
		t.Fatalf("global cap status=%d body=%s", status, raw)
	}
	code, _ := errorFields(t, raw)
	if code != "rate_limited" {
		t.Fatalf("code=%s", code)
	}
}

func TestFunnelEvents_RateLimitPerSession(t *testing.T) {
	db := openFunnelDB(t)
	h := NewFunnelEventHandler(funneleventuc.NewService(repository.NewFunnelEventRepository(db)))
	app := fiber.New()
	app.Post("/api/v1/funnel-events", middleware.FunnelEventSessionLimiter(2, time.Minute), h.Log)

	first := funnelBody(domain.FunnelCheckinPhotoStaged, "session-a", "/check-in", `{}`)
	for i := 0; i < 2; i++ {
		status, raw, _ := postFunnel(t, app, "", "", "", first)
		if status != http.StatusNoContent {
			t.Fatalf("request %d status=%d body=%s", i+1, status, raw)
		}
	}
	status, raw, _ := postFunnel(t, app, "", "", "", first)
	if status != http.StatusTooManyRequests {
		t.Fatalf("same session status=%d body=%s", status, raw)
	}
	other := funnelBody(domain.FunnelCheckinPhotoStaged, "session-b", "/check-in", `{}`)
	status, raw, _ = postFunnel(t, app, "", "", "", other)
	if status != http.StatusNoContent {
		t.Fatalf("other session status=%d body=%s", status, raw)
	}
}

func assertClientTS(t *testing.T, got time.Time) {
	t.Helper()
	want, err := time.Parse(time.RFC3339Nano, "2026-09-29T10:43:00.123Z")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("client_ts=%s want %s", got.UTC().Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

func assertProps(t *testing.T, raw json.RawMessage, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("props %s: %v", raw, err)
	}
	if len(got) != len(want) {
		t.Fatalf("props=%s", raw)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("props[%s]=%v (%T) want %v (%T)", k, got[k], got[k], v, v)
		}
	}
}
