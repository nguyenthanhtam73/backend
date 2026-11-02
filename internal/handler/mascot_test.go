package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func newMascotApp(t *testing.T) (*fiber.App, *gorm.DB, uuid.UUID) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:mascot_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.UserMascot{},
		&domain.MascotItem{},
		&domain.RoutineEntry{},
		&domain.SkinCheck{},
		&domain.SkinAnalysis{},
		&domain.Streak{},
	); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	u := &domain.User{Email: "mascot-http@test.com", Username: "mascot-" + uuid.NewString(), IsActive: true}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	h := NewMascotHandler(mascotuc.NewService(
		repository.NewMascotRepository(db),
		repository.NewSkinCheckRepository(db),
		repository.NewRoutineEntryRepository(db),
		repository.NewStreakRepository(db),
	))
	app := fiber.New()
	auth := func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalsUserID, u.ID)
		return c.Next()
	}
	app.Get("/api/v1/me/mascot", auth, h.Get)
	app.Put("/api/v1/me/mascot/loadout", auth, h.PutLoadout)
	app.Get("/api/v1/me/mascot-open", h.Get)
	return app, db, u.ID
}

func mascotCall(t *testing.T, app *fiber.App, method, path, body string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
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
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return res.StatusCode, env
}

func mascotData(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("data: %#v", env)
	}
	return data
}

func mascotCode(t *testing.T, env map[string]any) string {
	t.Helper()
	errBody, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("error: %#v", env)
	}
	code, _ := errBody["code"].(string)
	return code
}

func TestMascotGetCreatesDefaultAndPutValidates(t *testing.T) {
	app, db, _ := newMascotApp(t)

	status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot-open", "")
	if status != http.StatusUnauthorized || mascotCode(t, env) != "unauthorized" {
		t.Fatalf("open: %d %#v", status, env)
	}

	status, env = mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("get %d %#v", status, env)
	}
	data := mascotData(t, env)
	loadout, _ := data["loadout"].(map[string]any)
	if loadout["body"] != domain.MascotItemBodyDefault || data["mood"] != "happy" {
		t.Fatalf("default %#v", data)
	}
	if _, ok := data["messy_level"]; !ok {
		t.Fatal("messy_level missing")
	}
	if data["missed_days"] != float64(0) || data["messy_level"] != float64(0) {
		t.Fatalf("counters %#v", data)
	}
	marks, ok := data["skin_marks"].([]any)
	if !ok || len(marks) != 0 {
		t.Fatalf("marks %#v", data["skin_marks"])
	}

	status, env = mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatal(status)
	}
	var rows int64
	if err := db.Model(&domain.UserMascot{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("rows %d", rows)
	}

	price := 15
	if err := db.Create(&domain.MascotItem{ID: "outfit_gold", Layer: domain.MascotLayerOutfit, Name: "Áo vàng", IsFree: false, Price: &price}).Error; err != nil {
		t.Fatal(err)
	}
	bad := `{"body":"body_default","hair":"hair_default","outfit":"outfit_gold","accessory":"accessory_default","room_items":["room_plant"]}`
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", bad)
	if status != http.StatusBadRequest || mascotCode(t, env) != "item_not_free" {
		t.Fatalf("paid %d %#v", status, env)
	}

	wrong := `{"body":"hair_default","hair":"hair_default","outfit":"outfit_default","accessory":"","room_items":[]}`
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", wrong)
	if status != http.StatusBadRequest || mascotCode(t, env) != "wrong_layer" {
		t.Fatalf("layer %d %#v", status, env)
	}

	okBody := `{"body":"body_default","hair":"hair_default","outfit":"outfit_default","accessory":"","room_items":[]}`
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", okBody)
	if status != http.StatusOK {
		t.Fatalf("put %d %#v", status, env)
	}
	loadout, _ = mascotData(t, env)["loadout"].(map[string]any)
	rooms, _ := loadout["room_items"].([]any)
	if loadout["accessory"] != "" || len(rooms) != 0 {
		t.Fatalf("saved %#v", loadout)
	}
}

func TestMascotGetSleepyAfterTwoMissedDays(t *testing.T) {
	app, db, userID := newMascotApp(t)
	raw, _ := json.Marshal([]map[string]any{{"id": "wash", "title": "Rửa mặt", "completed": false}})
	row := &domain.RoutineEntry{
		UserID:      userID,
		RoutineDate: streaktime.Today().AddDate(0, 0, -2),
		Morning:     raw,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("%d %#v", status, env)
	}
	data := mascotData(t, env)
	if data["mood"] != "sleepy" || data["line_vi"] != "Hôm qua quên chăm da rồi nè" {
		t.Fatalf("mood %#v", data)
	}
	if data["line_en"] != "You forgot your skincare yesterday" {
		t.Fatalf("en %#v", data["line_en"])
	}
	if data["missed_days"] != float64(2) || data["messy_level"] != float64(0) {
		t.Fatalf("counters %#v", data)
	}
	routine, _ := data["routine_today"].(map[string]any)
	if routine["am_total"] != float64(1) || routine["am_done"] != float64(0) {
		t.Fatalf("today %#v", routine)
	}
}
