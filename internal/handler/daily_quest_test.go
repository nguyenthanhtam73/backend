package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/streaktime"
	mascotuc "github.com/dadiary/backend/internal/usecase/mascot"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDailyQuestClaimHTTP(t *testing.T) {
	app, db, user := newDailyQuestApp(t, true)
	late := time.Date(2026, 11, 5, 1, 15, 0, 0, streaktime.Location)
	app.h.questNow = func() time.Time { return late }
	if err := db.Create(&domain.RoutineEntry{
		UserID:      user,
		RoutineDate: time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC),
		Morning:     json.RawMessage(`[{"id":"wash","title":"Rửa mặt","completed":true}]`),
		Evening:     json.RawMessage(`[{"id":"cream","title":"Dưỡng","completed":false}]`),
	}).Error; err != nil {
		t.Fatal(err)
	}

	status, env := mascotCall(t, app.app, http.MethodPost, "/api/v1/mascot/daily-quest/claim", "")
	if status != http.StatusBadRequest || mascotCode(t, env) != "invalid_body" {
		t.Fatalf("empty body %d %#v", status, env)
	}
	status, env = mascotCall(t, app.app, http.MethodPost, "/api/v1/mascot/daily-quest/claim", `{"session":"evening","coins":100}`)
	if status != http.StatusBadRequest || mascotCode(t, env) != "invalid_body" {
		t.Fatalf("client amount %d %#v", status, env)
	}
	status, env = mascotCall(t, app.app, http.MethodPost, "/api/v1/mascot/daily-quest/claim", `{"session":"evening"}`)
	if status != http.StatusConflict || mascotCode(t, env) != "steps_not_done" {
		t.Fatalf("not done %d %#v", status, env)
	}

	status, env = mascotCall(t, app.app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("get %d %#v", status, env)
	}
	quest := dailyQuestFrom(t, env)
	morning := quest["morning"].(map[string]any)
	evening := quest["evening"].(map[string]any)
	amounts := quest["amounts"].(map[string]any)
	if morning["done"] != true || morning["claimed"] != false || evening["done"] != false || evening["claimed"] != false {
		t.Fatalf("status %#v", quest)
	}
	if amounts["morning"] != float64(8) || amounts["evening"] != float64(8) || amounts["bonus"] != float64(2) {
		t.Fatalf("amounts %#v", amounts)
	}
	if _, ok := mascotData(t, env)["loadout"]; !ok {
		t.Fatal("existing mascot fields missing")
	}

	status, env = mascotCall(t, app.app, http.MethodPost, "/api/v1/mascot/daily-quest/claim", `{"session":"morning"}`)
	if status != http.StatusOK {
		t.Fatalf("claim %d %#v", status, env)
	}
	data := mascotData(t, env)
	if data["granted"] != float64(8) || data["bonus"] != float64(0) || data["balance"] != float64(8) || data["already_claimed"] != false || data["session"] != "morning" {
		t.Fatalf("claim body %#v", data)
	}
	status, env = mascotCall(t, app.app, http.MethodPost, "/api/v1/mascot/daily-quest/claim", `{"session":"morning"}`)
	if status != http.StatusOK {
		t.Fatalf("replay %d %#v", status, env)
	}
	data = mascotData(t, env)
	if data["already_claimed"] != true || data["balance"] != float64(8) || data["granted"] != float64(8) {
		t.Fatalf("replay body %#v", data)
	}

	var claims int64
	if err := db.Model(&domain.DailyQuestClaim{}).Where("user_id = ? AND day = ?", user, "2026-11-04").Count(&claims).Error; err != nil {
		t.Fatal(err)
	}
	if claims != 1 {
		t.Fatalf("claim day rows %d", claims)
	}
}

func TestDailyQuestClaimRequiresMascotAccess(t *testing.T) {
	app, _, _ := newDailyQuestApp(t, false)
	status, env := mascotCall(t, app.app, http.MethodPost, "/api/v1/mascot/daily-quest/claim", `{"session":"morning"}`)
	if status != http.StatusNotFound || mascotCode(t, env) != "not_found" {
		t.Fatalf("closed %d %#v", status, env)
	}
	open := fiber.New()
	open.Post("/api/v1/mascot/daily-quest/claim", app.h.ClaimDailyQuest)
	status, env = mascotCall(t, open, http.MethodPost, "/api/v1/mascot/daily-quest/claim", `{"session":"morning"}`)
	if status != http.StatusUnauthorized || mascotCode(t, env) != "unauthorized" {
		t.Fatalf("anon %d %#v", status, env)
	}
}

type dailyQuestApp struct {
	app *fiber.App
	h   *MascotHandler
}

func newDailyQuestApp(t *testing.T, asAdmin bool) (*dailyQuestApp, *gorm.DB, uuid.UUID) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:daily_quest_http_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.UserMascot{},
		&domain.UserMascotItem{},
		&domain.MascotItem{},
		&domain.RoutineEntry{},
		&domain.SkinCheck{},
		&domain.SkinAnalysis{},
		&domain.Streak{},
		&domain.DailyQuestClaim{},
		&domain.UserCoinWallet{},
		&domain.UserCoinLedger{},
	); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	u := &domain.User{Email: "daily-quest@test.com", Username: "dq-" + uuid.NewString(), IsActive: true}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	var admins []string
	if asAdmin {
		admins = []string{u.Email}
	}
	h := NewMascotHandler(mascotuc.NewService(
		repository.NewMascotRepository(db),
		repository.NewSkinCheckRepository(db),
		repository.NewRoutineEntryRepository(db),
		repository.NewStreakRepository(db),
	), &config.Config{AdminEmails: admins})
	h.AttachDailyQuest(repository.NewDailyQuestRepository(db))
	app := fiber.New()
	auth := func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalsUserID, u.ID)
		c.Locals(middleware.LocalsUserEmail, u.Email)
		return c.Next()
	}
	app.Get("/api/v1/me/mascot", auth, h.Get)
	app.Post("/api/v1/mascot/daily-quest/claim", auth, h.ClaimDailyQuest)
	return &dailyQuestApp{app: app, h: h}, db, u.ID
}

func dailyQuestFrom(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	quest, ok := mascotData(t, env)["daily_quest"].(map[string]any)
	if !ok {
		t.Fatalf("daily_quest %#v", env)
	}
	return quest
}
