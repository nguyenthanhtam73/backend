package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	pushclickuc "github.com/dadiary/backend/internal/usecase/pushclick"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPushClick_RecordsAndDedupes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:pushclick_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.PushClickEvent{}); err != nil {
		t.Fatal(err)
	}
	uid := uuid.New()
	h := NewPushClickHandler(pushclickuc.NewService(repository.NewPushClickRepository(db)))
	app := fiber.New()
	app.Post("/api/v1/me/push/click", func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalsUserID, uid)
		return c.Next()
	}, h.Click)

	body := []byte(`{"kind":"d1_reminder","tag":"evening","idempotency_key":"sw-1"}`)
	post := func() *http.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/me/push/click", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	first := post()
	if first.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(first.Body)
		t.Fatalf("status=%d body=%s", first.StatusCode, b)
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Recorded  bool `json:"recorded"`
			Duplicate bool `json:"duplicate"`
		} `json:"data"`
	}
	if err := json.NewDecoder(first.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if !env.Success || !env.Data.Recorded || env.Data.Duplicate {
		t.Fatalf("first=%+v", env)
	}

	second := post()
	if second.StatusCode != http.StatusOK {
		t.Fatalf("dup status=%d", second.StatusCode)
	}
	env = struct {
		Success bool `json:"success"`
		Data    struct {
			Recorded  bool `json:"recorded"`
			Duplicate bool `json:"duplicate"`
		} `json:"data"`
	}{}
	if err := json.NewDecoder(second.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if !env.Data.Duplicate || env.Data.Recorded {
		t.Fatalf("second=%+v", env)
	}
	var n int64
	if err := db.Model(&domain.PushClickEvent{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rows=%d", n)
	}
	var row domain.PushClickEvent
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.UserID != uid || row.NotificationKind != "d1_reminder" || row.Tag != "evening" {
		t.Fatalf("row=%+v", row)
	}

	anon := fiber.New()
	anon.Post("/api/v1/me/push/click", h.Click)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/push/click", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := anon.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon status=%d", resp.StatusCode)
	}
}
