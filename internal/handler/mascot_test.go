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
	app.Post("/api/v1/me/mascot/intro", auth, h.SaveIntro)
	app.Delete("/api/v1/me/mascot/intro", auth, h.ResetIntro)
	app.Get("/api/v1/me/mascot-open", h.Get)
	app.Post("/api/v1/me/mascot-open/glow/seen", h.AckGlowSeen)
	app.Delete("/api/v1/me/mascot-open/intro", h.ResetIntro)
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

func TestMascotOwnedItems(t *testing.T) {
	app, db, userID := newMascotApp(t)
	if err := repository.SeedMascotCatalog(db); err != nil {
		t.Fatal(err)
	}
	clipItem := domain.MascotItem{ID: domain.TreeItemClipLeaf, Layer: domain.MascotLayerAccessory, Name: "Kẹp tóc lá xanh"}
	if err := db.Create(&clipItem).Error; err != nil {
		t.Fatal(err)
	}
	granted := time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)
	if err := db.Omit("User", "Item").Create(&domain.UserMascotItem{
		UserID: userID, ItemID: domain.TreeItemClipLeaf, Source: domain.MascotSourceRoomReward,
		AcquiredAt: granted,
	}).Error; err != nil {
		t.Fatal(err)
	}
	status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("get %d %#v", status, env)
	}
	raw, ok := mascotData(t, env)["owned_items"].([]any)
	if !ok || len(raw) == 0 {
		t.Fatalf("owned_items %#v", mascotData(t, env)["owned_items"])
	}
	var clip, free map[string]any
	for _, item := range raw {
		row, _ := item.(map[string]any)
		switch row["item_key"] {
		case domain.TreeItemClipLeaf:
			clip = row
		case domain.MascotItemBodyChibi:
			free = row
		}
	}
	if clip == nil || clip["layer"] != "accessory" || clip["preview_only"] != false || clip["source"] != "leaf/7" || clip["granted_at"] == "" {
		t.Fatalf("clip %#v", clip)
	}
	if free == nil || free["layer"] != "body" || free["source"] != "free" || free["preview_only"] != false {
		t.Fatalf("free %#v", free)
	}
	price := 12
	if err := db.Create(&domain.MascotItem{ID: "outfit_shop_ribbon", Layer: domain.MascotLayerOutfit, Name: "Nơ shop", Price: &price}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Omit("User", "Item").Create(&domain.UserMascotItem{
		UserID: userID, ItemID: "outfit_shop_ribbon", Source: domain.MascotSourceShop, AcquiredAt: granted.Add(time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	status, env = mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("shop get %d %#v", status, env)
	}
	raw, ok = mascotData(t, env)["owned_items"].([]any)
	if !ok {
		t.Fatal("owned_items missing")
	}
	var shopItem map[string]any
	for _, item := range raw {
		row, _ := item.(map[string]any)
		if row["item_key"] == "outfit_shop_ribbon" {
			shopItem = row
		}
	}
	if shopItem == nil || shopItem["source"] != "shop" || shopItem["layer"] != "outfit" {
		t.Fatalf("shop item %#v", shopItem)
	}
}

func TestLoadoutTopReplacesOutfitAndKeepsOmittedSlots(t *testing.T) {
	app, db, userID := newMascotApp(t)
	status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("get %d %#v", status, env)
	}
	for _, item := range []domain.MascotItem{
		{ID: "top_la_non", Layer: domain.MascotLayerTop, Name: "Áo thun Lá Non"},
		{ID: "bottom_quan_short_kem", Layer: domain.MascotLayerBottom, Name: "Quần short Kem Sữa"},
		{ID: "acc_no_la_xanh", Layer: domain.MascotLayerHairAcc, Name: "Nơ Lá Xanh"},
	} {
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Omit("User", "Item").Create(&domain.UserMascotItem{
			UserID: userID, ItemID: item.ID, Source: domain.MascotSourceGiftBox, AcquiredAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	body := `{"body":"body_chibi","hair":"hair_sleep_buns","outfit":"outfit_cozy_pajamas","top":"top_la_non","bottom":"bottom_quan_short_kem","accessory":"","room_items":[]}`
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", body)
	if status != http.StatusOK {
		t.Fatalf("put top %d %#v", status, env)
	}
	loadout, _ := mascotData(t, env)["loadout"].(map[string]any)
	if loadout["outfit"] != "" || loadout["top"] != "top_la_non" || loadout["bottom"] != "bottom_quan_short_kem" {
		t.Fatalf("split look %#v", loadout)
	}
	oldClient := `{"body":"body_chibi","hair":"hair_sleep_buns","outfit":"outfit_cozy_pajamas","accessory":"","room_items":[]}`
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", oldClient)
	if status != http.StatusOK {
		t.Fatalf("old put %d %#v", status, env)
	}
	loadout, _ = mascotData(t, env)["loadout"].(map[string]any)
	if loadout["outfit"] != "" || loadout["top"] != "top_la_non" || loadout["bottom"] != "bottom_quan_short_kem" {
		t.Fatalf("old client stripped the shirt %#v", loadout)
	}
	back := `{"body":"body_chibi","hair":"hair_sleep_buns","outfit":"outfit_cozy_pajamas","top":"","bottom":"","hair_acc":"acc_no_la_xanh","accessory":"","room_items":[]}`
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", back)
	if status != http.StatusOK {
		t.Fatalf("pajamas %d %#v", status, env)
	}
	loadout, _ = mascotData(t, env)["loadout"].(map[string]any)
	if loadout["outfit"] != "outfit_cozy_pajamas" || loadout["top"] != "" || loadout["bottom"] != "" || loadout["hair_acc"] != "acc_no_la_xanh" {
		t.Fatalf("back to outfit %#v", loadout)
	}
	status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", oldClient)
	if status != http.StatusOK {
		t.Fatalf("keep bow %d %#v", status, env)
	}
	loadout, _ = mascotData(t, env)["loadout"].(map[string]any)
	if loadout["hair_acc"] != "acc_no_la_xanh" || loadout["outfit"] != "outfit_cozy_pajamas" {
		t.Fatalf("omitted hair clip %#v", loadout)
	}
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

func TestMascotIntroRemembersGenderOnce(t *testing.T) {
	app, _, _ := newMascotApp(t)
	status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("get %d %#v", status, env)
	}
	data := mascotData(t, env)
	if data["gender"] != "female" || data["intro_seen"] != false {
		t.Fatalf("fresh %#v", data)
	}

	status, env = mascotCall(t, app, http.MethodPost, "/api/v1/me/mascot/intro", `{"gender":"male"}`)
	if status != http.StatusOK {
		t.Fatalf("intro %d %#v", status, env)
	}
	data = mascotData(t, env)
	if data["gender"] != "male" || data["intro_seen"] != true {
		t.Fatalf("saved %#v", data)
	}
	seen := data["intro_seen"]

	status, env = mascotCall(t, app, http.MethodPost, "/api/v1/me/mascot/intro", `{"gender":"nope"}`)
	if status != http.StatusBadRequest || mascotCode(t, env) != "invalid_gender" {
		t.Fatalf("bad gender %d %#v", status, env)
	}

	status, env = mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("reread %d %#v", status, env)
	}
	data = mascotData(t, env)
	if data["gender"] != "male" || data["intro_seen"] != seen {
		t.Fatalf("kept %#v", data)
	}
}

func TestMascotResetIntroAdminClearsSeenAndKeepsGender(t *testing.T) {
	app, db, userID := newMascotApp(t)

	status, env := mascotCall(t, app, http.MethodDelete, "/api/v1/me/mascot-open/intro", "")
	if status != http.StatusUnauthorized || mascotCode(t, env) != "unauthorized" {
		t.Fatalf("open %d %#v", status, env)
	}

	status, env = mascotCall(t, app, http.MethodDelete, "/api/v1/me/mascot/intro", "")
	if status != http.StatusOK {
		t.Fatalf("missing row %d %#v", status, env)
	}
	data := mascotData(t, env)
	if data["intro_seen"] != false || len(data) != 1 {
		t.Fatalf("missing body %#v", data)
	}
	var rows int64
	if err := db.Model(&domain.UserMascot{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("missing reset created %d rows", rows)
	}

	status, env = mascotCall(t, app, http.MethodPost, "/api/v1/me/mascot/intro", `{"gender":"male"}`)
	if status != http.StatusOK {
		t.Fatalf("intro %d %#v", status, env)
	}
	before := mascotStored(t, db, userID)
	if before.Gender != domain.MascotGenderMale || before.IntroSeenAt == nil {
		t.Fatalf("before %+v", before)
	}
	status, env = mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("seen get %d %#v", status, env)
	}
	seen := mascotData(t, env)
	if seen["gender"] != "male" || seen["intro_seen"] != true {
		t.Fatalf("seen %#v", seen)
	}

	status, env = mascotCallHeader(t, app, http.MethodDelete, "/api/v1/me/mascot/intro", "", map[string]string{
		"X-DaDiary-Client": "android-app",
	})
	if status != http.StatusOK {
		t.Fatalf("reset %d %#v", status, env)
	}
	data = mascotData(t, env)
	if data["intro_seen"] != false || len(data) != 1 {
		t.Fatalf("reset body %#v", data)
	}
	status, env = mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("after get %d %#v", status, env)
	}
	afterView := mascotData(t, env)
	if afterView["intro_seen"] != false || afterView["gender"] != "male" {
		t.Fatalf("after %#v", afterView)
	}
	if afterView["glow_level"] != seen["glow_level"] || afterView["loadout"] == nil {
		t.Fatalf("view changed %#v", afterView)
	}
	after := mascotStored(t, db, userID)
	if after.IntroSeenAt != nil || after.Gender != domain.MascotGenderMale {
		t.Fatalf("stored %+v", after)
	}
	if string(after.Loadout) != string(before.Loadout) || after.GlowLevel != before.GlowLevel || after.GlowLevelSeen != before.GlowLevelSeen {
		t.Fatalf("other columns changed before=%+v after=%+v", before, after)
	}

	status, env = mascotCall(t, app, http.MethodDelete, "/api/v1/me/mascot/intro", "")
	if status != http.StatusOK {
		t.Fatalf("second reset %d %#v", status, env)
	}
	if mascotData(t, env)["intro_seen"] != false {
		t.Fatalf("second body %#v", env)
	}
	again := mascotStored(t, db, userID)
	if again.IntroSeenAt != nil || again.Gender != domain.MascotGenderMale || string(again.Loadout) != string(before.Loadout) {
		t.Fatalf("second stored %+v", again)
	}
	status, env = mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK || mascotData(t, env)["intro_seen"] != false || mascotData(t, env)["gender"] != "male" {
		t.Fatalf("second get %d %#v", status, env)
	}
}

func TestMascotResetIntroHiddenFromNonAdmin(t *testing.T) {
	app, db, userID := newMascotAppFor(t, false)
	seen := time.Now().UTC()
	loadout, err := json.Marshal(domain.DefaultMascotLoadout())
	if err != nil {
		t.Fatal(err)
	}
	row := &domain.UserMascot{
		UserID:        userID,
		Loadout:       loadout,
		GlowLevel:     2,
		GlowLevelSeen: 1,
		Gender:        domain.MascotGenderMale,
		IntroSeenAt:   &seen,
	}
	if err := db.Omit("User").Create(row).Error; err != nil {
		t.Fatal(err)
	}

	status, env := mascotCall(t, app, http.MethodDelete, "/api/v1/me/mascot/intro", "")
	if status != http.StatusNotFound || mascotCode(t, env) != "not_found" {
		t.Fatalf("non-admin %d %#v", status, env)
	}
	status, env = mascotCallHeader(t, app, http.MethodDelete, "/api/v1/me/mascot/intro", "", map[string]string{
		"X-DaDiary-Client": "android-app",
	})
	if status != http.StatusNotFound || mascotCode(t, env) != "not_found" {
		t.Fatalf("trial header %d %#v", status, env)
	}

	got := mascotStored(t, db, userID)
	if got.IntroSeenAt == nil || got.Gender != domain.MascotGenderMale || got.GlowLevel != 2 || string(got.Loadout) != string(loadout) {
		t.Fatalf("row changed %+v", got)
	}
}

func mascotStored(t *testing.T, db *gorm.DB, userID uuid.UUID) domain.UserMascot {
	t.Helper()
	var row domain.UserMascot
	if err := db.Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestMascotFirstSkinMarksFromOnboarding(t *testing.T) {
	app, db, userID := newMascotApp(t)
	if err := db.AutoMigrate(&domain.SkinProfile{}); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"skin_analysis":{"concerns":["acne"],"severity_level":"mild","primary_regions":["cheeks"]}}`)
	profile := &domain.SkinProfile{UserID: userID, OnboardingSnapshot: raw}
	if err := repository.NewSkinProfileRepository(db).UpsertForUser(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	// The shared app's service has no profile reader. Rebuild the handler on the same db.
	cfg := &config.Config{AdminEmails: []string{"mascot-http@test.com"}}
	var email string
	if err := db.Model(&domain.User{}).Where("id = ?", userID).Pluck("email", &email).Error; err != nil {
		t.Fatal(err)
	}
	cfg.AdminEmails = []string{email}
	svc := mascotuc.NewService(
		repository.NewMascotRepository(db),
		repository.NewSkinCheckRepository(db),
		repository.NewRoutineEntryRepository(db),
		repository.NewStreakRepository(db),
	)
	svc.AttachProfiles(repository.NewSkinProfileRepository(db))
	h := NewMascotHandler(svc, cfg)
	gated := fiber.New()
	auth := func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalsUserID, userID)
		c.Locals(middleware.LocalsUserEmail, email)
		return c.Next()
	}
	gated.Get("/api/v1/me/mascot", auth, h.Get)
	status, env := mascotCall(t, gated, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("get %d %#v", status, env)
	}
	marks, _ := mascotData(t, env)["skin_marks"].([]any)
	if len(marks) != 1 {
		t.Fatalf("marks %#v", mascotData(t, env)["skin_marks"])
	}
	row, _ := marks[0].(map[string]any)
	if row["id"] != "acne" || row["level"] != "light" {
		t.Fatalf("mark %#v", row)
	}
	_ = app
}
