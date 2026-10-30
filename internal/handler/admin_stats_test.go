package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/token"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	piiEmail   = "pii-marker-9f3a@secret.test"
	piiUser    = "pii-user-none"
	piiUTM     = "utm-marker-zz9"
	piiFB      = "fbclid-marker-zz9"
	piiTT      = "ttclid-marker-zz9"
	adminEmail = "admin@example.com"
)

func newRegistrationStatsApp(t *testing.T) (*fiber.App, *gorm.DB, *token.Service) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:admin_stats_http_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}); err != nil {
		t.Fatal(err)
	}
	tok, err := token.NewService(config.JWTConfig{
		Secret:     "test-secret-for-admin-stats-32b!",
		AccessTTL:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		AdminEmails:      []string{adminEmail},
		SkinReviewEmails: []string{"reviewer@example.com"},
	}
	app := fiber.New()
	Router(app, cfg, db, tok, nil)
	return app, db, tok
}

func seedStatsUser(t *testing.T, db *gorm.DB, email, username string, at time.Time, source, fb, tt *string) *domain.User {
	t.Helper()
	u := &domain.User{
		Email:     email,
		Username:  username,
		IsActive:  true,
		UTMSource: source,
		FBCLID:    fb,
		TTCLID:    tt,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.NewUserRepository(db).SetCreatedAtForTest(context.Background(), u.ID, at); err != nil {
		t.Fatal(err)
	}
	return u
}

func bearer(t *testing.T, tok *token.Service, userID uuid.UUID) string {
	t.Helper()
	access, err := tok.SignAccess(userID)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + access
}

func getRegistrations(t *testing.T, app *fiber.App, auth, rawQuery string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/stats/registrations-by-source?"+rawQuery, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, body
}

func registrationStatsErrorCode(t *testing.T, raw []byte) string {
	t.Helper()
	var env struct {
		Success bool `json:"success"`
		Error   struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if env.Success {
		t.Fatalf("expected error envelope, got %s", raw)
	}
	return env.Error.Code
}

func TestRegistrationsBySource_RequiresAdmin(t *testing.T) {
	app, db, tok := newRegistrationStatsApp(t)
	day := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)
	secret := seedStatsUser(t, db, piiEmail, piiUser, day, strPtrStats(piiUTM), strPtrStats(piiFB), strPtrStats(piiTT))

	admin := seedStatsUser(t, db, adminEmail, "adminuser", day, nil, nil, nil)
	member := seedStatsUser(t, db, "member@example.com", "member", day, nil, nil, nil)
	reviewer := seedStatsUser(t, db, "reviewer@example.com", "reviewer", day, nil, nil, nil)

	cases := []struct {
		name   string
		auth   string
		status int
		code   string
	}{
		{name: "no header", status: http.StatusUnauthorized, code: "unauthorized"},
		{name: "empty bearer", auth: "Bearer ", status: http.StatusUnauthorized, code: "unauthorized"},
		{name: "bad token", auth: "Bearer not-a-jwt", status: http.StatusUnauthorized, code: "invalid_token"},
		{name: "member", auth: bearer(t, tok, member.ID), status: http.StatusForbidden, code: "forbidden"},
		{name: "skin review only", auth: bearer(t, tok, reviewer.ID), status: http.StatusForbidden, code: "forbidden"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw := getRegistrations(t, app, tc.auth, "since=2026-10-01")
			if status != tc.status {
				t.Fatalf("status=%d body=%s", status, raw)
			}
			if got := registrationStatsErrorCode(t, raw); got != tc.code {
				t.Fatalf("code=%s body=%s", got, raw)
			}
			assertNoPII(t, raw, secret.ID, admin.ID, member.ID, reviewer.ID)
		})
	}

	status, raw := getRegistrations(t, app, bearer(t, tok, admin.ID), "since=2026-10-01")
	if status != http.StatusOK {
		t.Fatalf("admin status=%d body=%s", status, raw)
	}
	if !strings.Contains(string(raw), `"success":true`) && !strings.Contains(string(raw), `"success": true`) {
		t.Fatalf("admin body=%s", raw)
	}
}

func TestRegistrationsBySource_GroupsAndOmitsPII(t *testing.T) {
	app, db, tok := newRegistrationStatsApp(t)
	day := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC) // 11:00 Asia/Ho_Chi_Minh
	old := time.Date(2026, 9, 15, 4, 0, 0, 0, time.UTC)

	admin := seedStatsUser(t, db, adminEmail, "adminuser", old, nil, nil, nil)
	tt := seedStatsUser(t, db, "tt-"+piiEmail, "ttuser", day, nil, nil, strPtrStats(piiTT))
	ttUTM := seedStatsUser(t, db, "ttutm@example.com", "ttutm", day, strPtrStats("TikTok"), nil, nil)
	both := seedStatsUser(t, db, "both@example.com", "bothids", day, nil, strPtrStats("IwAR-both"), strPtrStats("tt.both"))
	fb := seedStatsUser(t, db, "fb@example.com", "fbuser", day, nil, strPtrStats(piiFB), nil)
	ig := seedStatsUser(t, db, "ig@example.com", "iguser", day, strPtrStats("Instagram"), nil, nil)
	other := seedStatsUser(t, db, "other@example.com", "otheruser", day, strPtrStats(piiUTM), nil, nil)
	none := seedStatsUser(t, db, piiEmail, piiUser, day, nil, nil, nil)
	deleted := seedStatsUser(t, db, "deleted-"+piiEmail, "deleteduser", day, nil, nil, strPtrStats("tt.should-not-count"))
	if err := db.Delete(&domain.User{}, deleted.ID).Error; err != nil {
		t.Fatal(err)
	}
	seedStatsUser(t, db, "old@example.com", "olduser", old, strPtrStats("tiktok"), nil, nil)

	status, raw := getRegistrations(t, app, bearer(t, tok, admin.ID), "since=2026-10-01&tz=Asia/Ho_Chi_Minh")
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, raw)
	}

	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Timezone string `json:"timezone"`
			Since    string `json:"since"`
			Days     []struct {
				Date     string `json:"date"`
				TikTok   int64  `json:"tiktok"`
				Facebook int64  `json:"facebook"`
				Other    int64  `json:"other"`
				None     int64  `json:"none"`
			} `json:"days"`
		} `json:"data"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if !env.Success || env.Data.Timezone != "Asia/Ho_Chi_Minh" || env.Data.Since != "2026-10-01" {
		t.Fatalf("envelope: %s", raw)
	}
	if len(env.Data.Days) != 1 {
		t.Fatalf("days: %s", raw)
	}
	got := env.Data.Days[0]
	// ttclid, utm TikTok, and both click ids → tiktok. fbclid and Instagram → facebook.
	// utm-marker → other. blank → none. deleted and September tiktok stay out.
	// admin row is before since.
	if got.Date != "2026-10-15" || got.TikTok != 3 || got.Facebook != 2 || got.Other != 1 || got.None != 1 {
		t.Fatalf("counts=%+v body=%s", got, raw)
	}

	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(generic["data"], &data); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"email", "user_id", "id", "users", "fbclid", "ttclid", "utm_source"} {
		if _, ok := data[key]; ok {
			t.Fatalf("data has %s: %s", key, raw)
		}
	}

	assertNoPII(t, raw, admin.ID, tt.ID, ttUTM.ID, both.ID, fb.ID, ig.ID, other.ID, none.ID, deleted.ID)
}

func TestRegistrationsBySource_RejectsBadQuery(t *testing.T) {
	app, db, tok := newRegistrationStatsApp(t)
	admin := seedStatsUser(t, db, adminEmail, "adminuser", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), nil, nil, nil)
	auth := bearer(t, tok, admin.ID)

	status, raw := getRegistrations(t, app, auth, "")
	if status != http.StatusBadRequest || registrationStatsErrorCode(t, raw) != "invalid_since" {
		t.Fatalf("missing since status=%d body=%s", status, raw)
	}
	status, raw = getRegistrations(t, app, auth, "since=10-01-2026")
	if status != http.StatusBadRequest || registrationStatsErrorCode(t, raw) != "invalid_since" {
		t.Fatalf("bad since status=%d body=%s", status, raw)
	}
	status, raw = getRegistrations(t, app, auth, "since=2026-10-01&tz=Not/AZone")
	if status != http.StatusBadRequest || registrationStatsErrorCode(t, raw) != "invalid_timezone" {
		t.Fatalf("bad tz status=%d body=%s", status, raw)
	}
}

func strPtrStats(s string) *string { return &s }

func assertNoPII(t *testing.T, raw []byte, ids ...uuid.UUID) {
	t.Helper()
	body := string(raw)
	secrets := []string{
		piiEmail, piiUser, piiUTM, piiFB, piiTT, adminEmail,
		"member@example.com", "reviewer@example.com",
		"tt.should-not-count", "IwAR-both", "tt.both",
	}
	for _, id := range ids {
		secrets = append(secrets, id.String())
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(body, secret) {
			t.Fatalf("response contains %q: %s", secret, body)
		}
	}
}
