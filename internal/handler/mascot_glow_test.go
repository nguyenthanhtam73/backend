package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
	mascotuc "github.com/dadiary/backend/internal/usecase/mascot"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestMascotGlowCheckInDaysDedup(t *testing.T) {
	app, db, userID := newMascotApp(t)
	day := streaktime.Today()
	// Photo and skip on the same Vietnam civil day count once.
	insertMascotCheck(t, db, userID, day, `["checks/a.jpg"]`, day.Add(2*time.Hour))
	// created_at falls on the next calendar day; check_date does not.
	insertMascotCheck(t, db, userID, day, `[]`, day.Add(30*time.Hour))
	insertMascotCheck(t, db, userID, day.AddDate(0, 0, -1), `[]`, day.AddDate(0, 0, -1))
	// A removed check-in does not add a day.
	gone := insertMascotCheck(t, db, userID, day.AddDate(0, 0, -2), `["checks/old.jpg"]`, day.AddDate(0, 0, -2))
	if err := db.Delete(gone).Error; err != nil {
		t.Fatal(err)
	}

	status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("get %d %#v", status, env)
	}
	data := mascotData(t, env)
	const days = 2
	level := mascotuc.GlowLevel(days)
	if data["checkin_days"] != float64(days) || data["glow_level"] != float64(level) || data["next_level_at"] != glowNextJSON(level) || data["level_up"] != (level > 1) {
		t.Fatalf("glow %#v", data)
	}
}

func TestMascotGlowThresholdsAndNeverDecreases(t *testing.T) {
	app, db, userID := newMascotApp(t)
	assertMascotGlow(t, app, 1, 0, glowNextJSON(1), false)

	have := 0
	start := streaktime.Today()
	seedTo := func(n int) {
		t.Helper()
		for have < n {
			have++
			day := start.AddDate(0, 0, -(have - 1))
			images := `[]`
			if have%2 == 0 {
				images = `["checks/p.jpg"]`
			}
			insertMascotCheck(t, db, userID, day, images, day)
		}
	}
	for _, at := range mascotuc.GlowThresholds {
		if at > 0 {
			seedTo(at - 1)
			level := mascotuc.GlowLevel(at - 1)
			assertMascotGlow(t, app, level, at-1, glowNextJSON(level), level > 1)
		}
		seedTo(at)
		level := mascotuc.GlowLevel(at)
		assertMascotGlow(t, app, level, at, glowNextJSON(level), level > 1)
	}

	top := len(mascotuc.GlowThresholds) + 1
	topDays := 0
	if len(mascotuc.GlowThresholds) > 0 {
		topDays = mascotuc.GlowThresholds[len(mascotuc.GlowThresholds)-1]
	}
	var row domain.UserMascot
	if err := db.Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.GlowLevel != top {
		t.Fatalf("stored %d want %d", row.GlowLevel, top)
	}

	status, env := mascotCall(t, app, http.MethodPost, "/api/v1/me/mascot/glow/seen", "")
	if status != http.StatusOK {
		t.Fatalf("seen %d %#v", status, env)
	}
	assertMascotGlow(t, app, top, topDays, nil, false)
	status, env = mascotCall(t, app, http.MethodPost, "/api/v1/me/mascot/glow/seen", "")
	if status != http.StatusOK {
		t.Fatalf("seen again %d %#v", status, env)
	}
	assertMascotGlow(t, app, top, topDays, nil, false)
	if err := db.Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.GlowLevelSeen != top || row.GlowLevel != top {
		t.Fatalf("after ack %+v", row)
	}

	if err := db.Where("user_id = ?", userID).Delete(&domain.SkinCheck{}).Error; err != nil {
		t.Fatal(err)
	}
	assertMascotGlow(t, app, top, 0, nil, false)
	if err := db.Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.GlowLevel != top {
		t.Fatalf("stored fell to %d", row.GlowLevel)
	}

	if top == 1 {
		return
	}
	// A stored level above the recomputed one is what GET returns.
	below := top - 1
	if err := db.Model(&domain.UserMascot{}).Where("user_id = ?", userID).Update("glow_level", below).Error; err != nil {
		t.Fatal(err)
	}
	assertMascotGlow(t, app, below, 0, glowNextJSON(below), false)
	if err := db.Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.GlowLevel != below {
		t.Fatalf("read wrote level down to %d", row.GlowLevel)
	}
}

func TestMascotGlowUnlocksAccessories(t *testing.T) {
	head := domain.MascotItemAccessoryPinkHeadband
	star := domain.MascotItemAccessoryStarClip
	base := `{"body":"body_chibi","hair":"hair_sleep_buns","outfit":"outfit_cozy_pajamas","room_items":["room_plant"]}`
	headDays := mascotuc.DaysForGlowLevel(3)
	starDays := mascotuc.DaysForGlowLevel(4)
	if mascotuc.GlowLevel(headDays) != 3 || mascotuc.GlowLevel(starDays) != 4 {
		t.Fatalf("GlowThresholds cannot reach accessory levels: head %d star %d", headDays, starDays)
	}

	t.Run("free locked until level", func(t *testing.T) {
		app, db, userID := newMascotApp(t)
		status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
		if status != http.StatusOK {
			t.Fatal(status)
		}
		if acc := mascotAccessories(t, env); len(acc) != 0 {
			t.Fatalf("level 1 default %#v", acc)
		}
		for _, id := range []string{head, star} {
			body := mascotAccessoryBody(base, id)
			status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", body)
			if status != http.StatusBadRequest || mascotCode(t, env) != "item_locked" {
				t.Fatalf("put %s %d %#v", id, status, env)
			}
		}

		seedMascotCheckDays(t, db, userID, headDays)
		status, env = mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
		if status != http.StatusOK {
			t.Fatal(status)
		}
		acc := mascotAccessories(t, env)
		if len(acc) != 1 || acc[0] != head {
			t.Fatalf("level 3 default %#v", acc)
		}
		status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", mascotAccessoryBody(base, head))
		if status != http.StatusOK {
			t.Fatalf("headband %d %#v", status, env)
		}
		status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", mascotAccessoryBody(base, star))
		if status != http.StatusBadRequest || mascotCode(t, env) != "item_locked" {
			t.Fatalf("star at 3 %d %#v", status, env)
		}

		seedMascotCheckDays(t, db, userID, starDays)
		status, env = mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
		if status != http.StatusOK {
			t.Fatal(status)
		}
		// The earlier PUT stored only the headband, so the star is not put back.
		acc = mascotAccessories(t, env)
		if len(acc) != 1 || acc[0] != head {
			t.Fatalf("after headband save %#v", acc)
		}
		status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", mascotBothAccessories(base))
		if status != http.StatusOK {
			t.Fatalf("both at 4 %d %#v", status, env)
		}
		acc = mascotAccessories(t, env)
		if len(acc) != 2 || acc[0] != head || acc[1] != star {
			t.Fatalf("equipped %#v", acc)
		}
		data := mascotData(t, env)
		if data["glow_level"] != float64(4) || data["checkin_days"] != float64(starDays) || data["next_level_at"] != glowNextJSON(4) {
			t.Fatalf("glow %#v", data)
		}
	})

	t.Run("default loadout shows them as they unlock", func(t *testing.T) {
		app, db, userID := newMascotApp(t)
		seedMascotCheckDays(t, db, userID, starDays)
		status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
		if status != http.StatusOK {
			t.Fatal(status)
		}
		acc := mascotAccessories(t, env)
		if len(acc) != 2 || acc[0] != head || acc[1] != star {
			t.Fatalf("level 4 default %#v", acc)
		}
		data := mascotData(t, env)
		if data["glow_level"] != float64(4) || data["checkin_days"] != float64(starDays) || data["level_up"] != true {
			t.Fatalf("glow %#v", data)
		}
	})

	t.Run("shop wears before the level", func(t *testing.T) {
		app, db, userID := newMascotApp(t)
		if _, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", ""); mascotData(t, env)["glow_level"] != float64(1) {
			t.Fatal("expected level 1")
		}
		for _, id := range []string{head, star} {
			res := db.Model(&domain.UserMascotItem{}).
				Where("user_id = ? AND item_id = ?", userID, id).
				Update("source", domain.MascotSourceShop)
			if res.Error != nil || res.RowsAffected != 1 {
				t.Fatalf("source %s %d %v", id, res.RowsAffected, res.Error)
			}
		}
		status, env := mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", mascotBothAccessories(base))
		if status != http.StatusOK {
			t.Fatalf("shop put %d %#v", status, env)
		}
		acc := mascotAccessories(t, env)
		if len(acc) != 2 || acc[0] != head || acc[1] != star {
			t.Fatalf("shop loadout %#v", acc)
		}
		if mascotData(t, env)["glow_level"] != float64(1) {
			t.Fatalf("level %#v", mascotData(t, env)["glow_level"])
		}
	})

	t.Run("room reward does not skip the level", func(t *testing.T) {
		app, db, userID := newMascotApp(t)
		status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
		if status != http.StatusOK {
			t.Fatal(status)
		}
		if err := db.Model(&domain.UserMascotItem{}).
			Where("user_id = ? AND item_id = ?", userID, head).
			Update("source", domain.MascotSourceRoomReward).Error; err != nil {
			t.Fatal(err)
		}
		status, env = mascotCall(t, app, http.MethodPut, "/api/v1/me/mascot/loadout", mascotAccessoryBody(base, head))
		if status != http.StatusBadRequest || mascotCode(t, env) != "item_locked" {
			t.Fatalf("reward %d %#v", status, env)
		}
	})
}

func glowNextJSON(level int) any {
	n := mascotuc.NextLevelAt(level)
	if n == nil {
		return nil
	}
	return float64(*n)
}

func assertMascotGlow(t *testing.T, app *fiber.App, level, days int, next any, levelUp bool) {
	t.Helper()
	status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("get %d %#v", status, env)
	}
	data := mascotData(t, env)
	if data["glow_level"] != float64(level) || data["checkin_days"] != float64(days) || data["level_up"] != levelUp {
		t.Fatalf("glow want level=%d days=%d up=%v got %#v", level, days, levelUp, data)
	}
	if data["next_level_at"] != next {
		t.Fatalf("next %#v want %#v", data["next_level_at"], next)
	}
}

func mascotAccessories(t *testing.T, env map[string]any) []any {
	t.Helper()
	data := mascotData(t, env)
	loadout, _ := data["loadout"].(map[string]any)
	acc, _ := loadout["accessories"].([]any)
	return acc
}

func mascotAccessoryBody(base, id string) string {
	return base[:len(base)-1] + `,"accessory":"` + id + `","accessories":["` + id + `"]}`
}

func mascotBothAccessories(base string) string {
	head := domain.MascotItemAccessoryPinkHeadband
	star := domain.MascotItemAccessoryStarClip
	return base[:len(base)-1] + `,"accessory":"` + head + `","accessories":["` + head + `","` + star + `"]}`
}

func seedMascotCheckDays(t *testing.T, db *gorm.DB, userID uuid.UUID, n int) {
	t.Helper()
	start := streaktime.Today()
	for i := 0; i < n; i++ {
		day := start.AddDate(0, 0, -i)
		images := `[]`
		if i%2 == 1 {
			images = `["checks/p.jpg"]`
		}
		insertMascotCheck(t, db, userID, day, images, day)
	}
}

func insertMascotCheck(t *testing.T, db *gorm.DB, userID uuid.UUID, day time.Time, images string, created time.Time) *domain.SkinCheck {
	t.Helper()
	row := &domain.SkinCheck{
		UserID:     userID,
		ImageURLs:  json.RawMessage(images),
		CheckDate:  streaktime.DateOf(day),
		Visibility: domain.CheckVisibilityPrivate,
		ClientKind: domain.RefreshClientWeb,
		CreatedAt:  created.UTC(),
		UpdatedAt:  created.UTC(),
	}
	if err := db.Omit("User", "Analysis").Create(row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}
