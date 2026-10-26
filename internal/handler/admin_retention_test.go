package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/token"
	adminretentionuc "github.com/dadiary/backend/internal/usecase/adminretention"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAdminRetention_InvalidDate(t *testing.T) {
	app := fiber.New()
	h := NewAdminRetentionHandler(adminretentionuc.NewService(nil), nil)
	app.Get("/api/v1/admin/retention-stats", h.Get)

	for _, path := range []string{
		"/api/v1/admin/retention-stats?from=2026-02-31",
		"/api/v1/admin/retention-stats?from=2026-09-08&to=2026-09-01",
		"/api/v1/admin/retention-stats?to=09-02-2026",
	} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", path, resp.StatusCode, body)
		}
		var env struct {
			Success bool `json:"success"`
			Error   struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatal(err)
		}
		if env.Success || env.Error.Code != "invalid_date" {
			t.Fatalf("%s envelope %+v", path, env)
		}
	}
}

func TestAdminRetention_RouteAuthAndEmptyStats(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:admin_retention_http_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.SkinCheck{}); err != nil {
		t.Fatal(err)
	}
	tok, err := token.NewService(config.JWTConfig{
		Secret:     "test-secret-for-retention-stats-32",
		AccessTTL:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		AdminEmails:      []string{"founder@dadiary.vn"},
		SkinReviewEmails: []string{"reviewer@dadiary.vn"},
	}
	founder := &domain.User{Email: "founder@dadiary.vn", Username: "founder", IsActive: true}
	member := &domain.User{Email: "member@dadiary.test", Username: "member", IsActive: true}
	if err := db.Create(founder).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(member).Error; err != nil {
		t.Fatal(err)
	}
	// One check-in so an unfiltered count would be non-zero. The founder is excluded.
	if err := db.Create(&domain.SkinCheck{
		UserID:    member.ID,
		ImageURLs: []byte(`["a.jpg"]`),
		CheckDate: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	Router(app, cfg, db, tok, nil)

	anon, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/admin/retention-stats", nil))
	if err != nil {
		t.Fatal(err)
	}
	if anon.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(anon.Body)
		t.Fatalf("anon status=%d body=%s", anon.StatusCode, body)
	}

	memberTok, err := tok.SignAccess(member.ID)
	if err != nil {
		t.Fatal(err)
	}
	memberReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/retention-stats", nil)
	memberReq.Header.Set("Authorization", "Bearer "+memberTok)
	forbidden, err := app.Test(memberReq)
	if err != nil {
		t.Fatal(err)
	}
	if forbidden.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(forbidden.Body)
		t.Fatalf("member status=%d body=%s", forbidden.StatusCode, body)
	}

	adminTok, err := tok.SignAccess(founder.ID)
	if err != nil {
		t.Fatal(err)
	}
	adminReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/retention-stats", nil)
	adminReq.Header.Set("Authorization", "Bearer "+adminTok)
	ok, err := app.Test(adminReq)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(ok.Body)
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("admin status=%d body=%s", ok.StatusCode, body)
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			RegisteredUsers  int64 `json:"registered_users"`
			UsersWithCheckin int64 `json:"users_with_checkin"`
			DaysUsed         struct {
				AtLeast1 int64 `json:"at_least_1"`
				MaxDays  int64 `json:"max_days"`
			} `json:"days_used"`
			BySignupWeek []any `json:"by_signup_week"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatal(err)
	}
	if !env.Success || env.Data.RegisteredUsers != 1 || env.Data.UsersWithCheckin != 1 || env.Data.DaysUsed.AtLeast1 != 1 || env.Data.DaysUsed.MaxDays != 1 {
		t.Fatalf("body %s", body)
	}
	if env.Data.BySignupWeek == nil {
		t.Fatal("by_signup_week is null")
	}
}
