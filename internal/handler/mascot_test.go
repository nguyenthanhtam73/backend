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
	return newMascotAppFor(t, true)
}

func newMascotAppFor(t *testing.T, asAdmin bool) (*fiber.App, *gorm.DB, uuid.UUID) {
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
		&domain.UserMascotItem{},
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
	var admins []string
	if asAdmin {
		admins = []string{u.Email}
	}
	cfg := &config.Config{AdminEmails: admins}
	h := NewMascotHandler(mascotuc.NewService(
		repository.NewMascotRepository(db),
		repository.NewSkinCheckRepository(db),
		repository.NewRoutineEntryRepository(db),
		repository.NewStreakRepository(db),
	), cfg)
	app := fiber.New()
	auth := func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalsUserID, u.ID)
		c.Locals(middleware.LocalsUserEmail, u.Email)
		return c.Next()
	}
	app.Get("/api/v1/me/mascot", auth, h.Get)
	app.Put("/api/v1/me/mascot/loadout", auth, h.PutLoadout)
	app.Post("/api/v1/me/mascot/glow/seen", auth, h.AckGlowSeen)
	app.Get("/api/v1/me/mascot-open", h.Get)
	app.Post("/api/v1/me/mascot-open/glow/seen", h.AckGlowSeen)
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
	status, env = mascotCall(t, app, http.MethodPost, "/api/v1/me/mascot-open/glow/seen", "")
	if status != http.StatusUnauthorized || mascotCode(t, env) != "unauthorized" {
		t.Fatalf("open seen: %d %#v", status, env)
	}

	status, env = mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("get %d %#v", status, env)
	}
	data := mascotData(t, env)
	loadout, _ := data["loadout"].(map[string]any)
	if loadout["body"] != domain.MascotItemBodyChibi || data["mood"] != "happy" {
		t.Fatalf("default %#v", data)
	}
	// Level 1 keeps the free headband and star clip in the stored default,
	// but they are not shown until glow 3 and 4.
	acc, _ := loadout["accessories"].([]any)
	if loadout["accessory"] != "" || len(acc) != 0 {
		t.Fatalf("locked default accessories %#v", loadout)
	}
	if data["glow_level"] != float64(1) || data["glow_points"] != float64(0) || data["level_up"] != false || data["near_level_up"] != false {
		t.Fatalf("glow %#v", data)
	}
	if data["next_level_points"] != glowNextJSON(1) || data["glow_progress"] != float64(0) {
		t.Fatalf("next %#v progress %#v", data["next_level_points"], data["glow_progress"])
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
	bad := `{"body":"body_chibi","hair":"hair_sleep_buns","outfit":"outfit_gold","accessory":"accessory_star_clip","room_items":["room_plant"]}`
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", bad)
	if status != http.StatusBadRequest || mascotCode(t, env) != "item_not_owned" {
		t.Fatalf("paid %d %#v", status, env)
	}

	wrong := `{"body":"hair_sleep_buns","hair":"hair_sleep_buns","outfit":"outfit_cozy_pajamas","accessory":"","room_items":[]}`
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", wrong)
	if status != http.StatusBadRequest || mascotCode(t, env) != "wrong_layer" {
		t.Fatalf("layer %d %#v", status, env)
	}

	okBody := `{"body":"body_chibi","hair":"hair_sleep_buns","outfit":"outfit_cozy_pajamas","accessory":"","room_items":[]}`
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
	raw, _ := json.Marshal([]map[string]any{{"id": "wash", "title": "Rửa mặt", "completed": true}})
	careDay := streaktime.Today().AddDate(0, 0, -2)
	row := &domain.RoutineEntry{
		UserID:      userID,
		RoutineDate: careDay,
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
	if data["last_care_at"] != careDay.Format("2006-01-02") {
		t.Fatalf("last_care_at %#v", data["last_care_at"])
	}
	if data["last_checkin_at"] != nil {
		t.Fatalf("full routine created a check-in timestamp %#v", data["last_checkin_at"])
	}
	routine, _ := data["routine_today"].(map[string]any)
	if routine["am_total"] != float64(1) || routine["am_done"] != float64(0) {
		t.Fatalf("today %#v", routine)
	}
}

func TestMascotMissedDaysFollowLatestCare(t *testing.T) {
	fullMorning := `[{"id":"wash","title":"Rửa mặt","completed":true}]`
	fullEvening := `[{"id":"cream","title":"Dưỡng","completed":true}]`
	partialMorning := `[{"id":"wash","title":"Rửa mặt","completed":true},{"id":"spf","title":"Chống nắng","completed":false}]`

	t.Run("check-in newer", func(t *testing.T) {
		app, db, userID := newMascotApp(t)
		today := streaktime.Today()
		checkDay := today.AddDate(0, 0, -1)
		// 17:30 UTC is 00:30 the next day in Asia/Ho_Chi_Minh. The miss uses
		// check_date, the same civil day as the streak, not that timestamp.
		created := time.Date(checkDay.Year(), checkDay.Month(), checkDay.Day(), 17, 30, 0, 0, time.UTC)
		insertMascotCheck(t, db, userID, checkDay, `["checks/face.jpg"]`, created)
		careDay := today.AddDate(0, 0, -5)
		insertMascotRoutine(t, db, userID, careDay, fullMorning, fullEvening)

		var checksBefore int64
		if err := db.Model(&domain.SkinCheck{}).Where("user_id = ?", userID).Count(&checksBefore).Error; err != nil {
			t.Fatal(err)
		}
		status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
		if status != http.StatusOK {
			t.Fatalf("%d %#v", status, env)
		}
		data := mascotData(t, env)
		if data["missed_days"] != float64(1) || data["mood"] != "normal" {
			t.Fatalf("check-in newer %#v", data)
		}
		if data["last_care_at"] != careDay.Format("2006-01-02") {
			t.Fatalf("last_care_at %#v", data["last_care_at"])
		}
		var checksAfter int64
		if err := db.Model(&domain.SkinCheck{}).Where("user_id = ?", userID).Count(&checksAfter).Error; err != nil {
			t.Fatal(err)
		}
		if checksAfter != checksBefore {
			t.Fatalf("checks %d to %d", checksBefore, checksAfter)
		}
		if _, ok := data["last_checkin_at"].(string); !ok {
			t.Fatalf("last_checkin_at %#v", data["last_checkin_at"])
		}
		per := float64(mascotuc.GlowConfig.CheckInPoints + mascotuc.GlowConfig.RoutineBonus)
		if data["glow_points"] != per {
			t.Fatalf("glow %#v", data["glow_points"])
		}
	})

	t.Run("routine newer", func(t *testing.T) {
		app, db, userID := newMascotApp(t)
		today := streaktime.Today()
		checkDay := today.AddDate(0, 0, -5)
		created := checkDay.Add(9 * time.Hour)
		insertMascotCheck(t, db, userID, checkDay, `[]`, created)
		careDay := today.AddDate(0, 0, -1)
		insertMascotRoutine(t, db, userID, careDay, fullMorning, fullEvening)
		streakDay := streaktime.DateOf(checkDay)
		if err := db.Omit("User").Create(&domain.Streak{
			UserID:           userID,
			CurrentStreak:    5,
			LongestStreak:    5,
			LastCheckInDate:  &streakDay,
			FreezesAvailable: 1,
		}).Error; err != nil {
			t.Fatal(err)
		}

		status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
		if status != http.StatusOK {
			t.Fatalf("%d %#v", status, env)
		}
		data := mascotData(t, env)
		if data["missed_days"] != float64(1) || data["mood"] != "normal" {
			t.Fatalf("routine newer %#v", data)
		}
		if data["last_care_at"] != careDay.Format("2006-01-02") {
			t.Fatalf("last_care_at %#v", data["last_care_at"])
		}
		if data["streak_days"] != float64(0) {
			t.Fatalf("routine must not keep the check-in streak alive: %#v", data["streak_days"])
		}
		var streak domain.Streak
		if err := db.Where("user_id = ?", userID).First(&streak).Error; err != nil {
			t.Fatal(err)
		}
		if streak.CurrentStreak != 5 || streak.LastCheckInDate == nil || streak.LastCheckInDate.UTC().Format("2006-01-02") != streakDay.Format("2006-01-02") {
			t.Fatalf("streak row changed: %+v", streak)
		}
		var checks int64
		if err := db.Model(&domain.SkinCheck{}).Where("user_id = ?", userID).Count(&checks).Error; err != nil {
			t.Fatal(err)
		}
		if checks != 1 {
			t.Fatalf("check rows %d", checks)
		}
		per := float64(mascotuc.GlowConfig.CheckInPoints + mascotuc.GlowConfig.RoutineBonus)
		if data["glow_points"] != per {
			t.Fatalf("glow %#v", data["glow_points"])
		}
	})

	t.Run("partial routine does not count", func(t *testing.T) {
		app, db, userID := newMascotApp(t)
		today := streaktime.Today()
		careDay := today.AddDate(0, 0, -3)
		insertMascotRoutine(t, db, userID, careDay, fullMorning, fullEvening)
		insertMascotRoutine(t, db, userID, today.AddDate(0, 0, -1), partialMorning, `[]`)
		insertMascotRoutine(t, db, userID, today, partialMorning, fullEvening)

		status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
		if status != http.StatusOK {
			t.Fatalf("%d %#v", status, env)
		}
		data := mascotData(t, env)
		if data["missed_days"] != float64(3) || data["mood"] != "sleepy" {
			t.Fatalf("partial %#v", data)
		}
		if data["last_care_at"] != careDay.Format("2006-01-02") {
			t.Fatalf("last_care_at %#v", data["last_care_at"])
		}
		if data["last_checkin_at"] != nil {
			t.Fatalf("last_checkin_at %#v", data["last_checkin_at"])
		}
		if data["glow_points"] != float64(mascotuc.GlowConfig.RoutineBonus) {
			t.Fatalf("partial must not add glow %#v", data["glow_points"])
		}
		var checks int64
		if err := db.Model(&domain.SkinCheck{}).Where("user_id = ?", userID).Count(&checks).Error; err != nil {
			t.Fatal(err)
		}
		if checks != 0 {
			t.Fatalf("partial created %d checks", checks)
		}
	})

	t.Run("none", func(t *testing.T) {
		app, db, userID := newMascotApp(t)
		status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
		if status != http.StatusOK {
			t.Fatalf("%d %#v", status, env)
		}
		data := mascotData(t, env)
		if _, ok := data["last_care_at"]; !ok {
			t.Fatal("last_care_at missing")
		}
		if data["missed_days"] != float64(0) || data["mood"] != "happy" || data["last_care_at"] != nil || data["last_checkin_at"] != nil {
			t.Fatalf("none %#v", data)
		}
		if data["glow_points"] != float64(0) || data["streak_days"] != float64(0) {
			t.Fatalf("none counters %#v", data)
		}
		var checks, routines int64
		if err := db.Model(&domain.SkinCheck{}).Where("user_id = ?", userID).Count(&checks).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&domain.RoutineEntry{}).Where("user_id = ?", userID).Count(&routines).Error; err != nil {
			t.Fatal(err)
		}
		if checks != 0 || routines != 0 {
			t.Fatalf("checks %d routines %d", checks, routines)
		}
	})
}

func TestMascotHiddenFromNonAdmin(t *testing.T) {
	app, db, _ := newMascotAppFor(t, false)
	status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusNotFound || mascotCode(t, env) != "not_found" {
		t.Fatalf("get %d %#v", status, env)
	}
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", `{"body":"body_chibi"}`)
	if status != http.StatusNotFound {
		t.Fatalf("put %d %#v", status, env)
	}
	status, env = mascotCall(t, app, http.MethodPost, "/api/v1/me/mascot/glow/seen", "")
	if status != http.StatusNotFound || mascotCode(t, env) != "not_found" {
		t.Fatalf("seen %d %#v", status, env)
	}
	var mascots, grants int64
	if err := db.Model(&domain.UserMascot{}).Count(&mascots).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.UserMascotItem{}).Count(&grants).Error; err != nil {
		t.Fatal(err)
	}
	if mascots != 0 || grants != 0 {
		t.Fatalf("non-admin wrote mascot=%d grants=%d", mascots, grants)
	}
}

func TestMascotSeedsFreeGrantsAndSkipsExpired(t *testing.T) {
	app, db, userID := newMascotApp(t)
	status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("get %d %#v", status, env)
	}
	var grants []domain.UserMascotItem
	if err := db.Where("user_id = ?", userID).Find(&grants).Error; err != nil {
		t.Fatal(err)
	}
	if len(grants) != len(domain.DefaultMascotItems()) {
		t.Fatalf("grants %d", len(grants))
	}
	for _, g := range grants {
		if g.Source != domain.MascotSourceFree || g.ExpiresAt != nil || g.SourceRoomID != nil {
			t.Fatalf("grant %+v", g)
		}
	}

	past := time.Now().UTC().Add(-time.Hour)
	room := "room_week_1"
	if err := db.Create(&domain.MascotItem{ID: "bow_temp", Layer: domain.MascotLayerAccessory, Name: "Nơ tạm", IsFree: false}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Omit("User", "Item").Create(&domain.UserMascotItem{
		UserID:       userID,
		ItemID:       "bow_temp",
		Source:       domain.MascotSourceRoomReward,
		SourceRoomID: &room,
		AcquiredAt:   past,
		ExpiresAt:    &past,
	}).Error; err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(domain.MascotLoadout{
		Body: domain.MascotItemBodyChibi, Hair: domain.MascotItemHairSleepBuns,
		Outfit: domain.MascotItemOutfitCozyPajamas, Accessory: "bow_temp",
		RoomItems: []string{domain.MascotItemRoomPlant},
	})
	if err := db.Model(&domain.UserMascot{}).Where("user_id = ?", userID).Updates(map[string]any{
		"loadout":    raw,
		"glow_level": len(mascotuc.GlowConfig.Thresholds) + 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	status, env = mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("get expired %d %#v", status, env)
	}
	loadout, _ := mascotData(t, env)["loadout"].(map[string]any)
	acc, _ := loadout["accessories"].([]any)
	if loadout["accessory"] != domain.MascotItemAccessoryPinkHeadband || len(acc) != 2 {
		t.Fatalf("expired accessory should fall back to the free pair: %#v", loadout)
	}
	var still int64
	if err := db.Model(&domain.UserMascotItem{}).Where("item_id = ?", "bow_temp").Count(&still).Error; err != nil {
		t.Fatal(err)
	}
	if still != 1 {
		t.Fatal("expired grant was deleted")
	}

	body := `{"body":"body_chibi","hair":"hair_sleep_buns","outfit":"outfit_cozy_pajamas","accessory":"bow_temp","room_items":[]}`
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", body)
	if status != http.StatusBadRequest || mascotCode(t, env) != "item_not_owned" {
		t.Fatalf("put expired %d %#v", status, env)
	}

	future := time.Now().UTC().Add(72 * time.Hour)
	if err := db.Model(&domain.UserMascotItem{}).Where("item_id = ?", "bow_temp").Update("expires_at", future).Error; err != nil {
		t.Fatal(err)
	}
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", body)
	if status != http.StatusOK {
		t.Fatalf("put active reward %d %#v", status, env)
	}
	loadout, _ = mascotData(t, env)["loadout"].(map[string]any)
	if loadout["accessory"] != "bow_temp" {
		t.Fatalf("reward not equipped: %#v", loadout)
	}
}
