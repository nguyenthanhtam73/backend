package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
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
	if data["checkin_days"] != float64(2) || data["glow_level"] != float64(1) || data["next_level_at"] != float64(3) || data["level_up"] != false {
		t.Fatalf("glow %#v", data)
	}
}

func TestMascotGlowThresholdsAndNeverDecreases(t *testing.T) {
	app, db, userID := newMascotApp(t)
	assertMascotGlow(t, app, 1, 0, float64(3), false)

	steps := []struct {
		days  int
		level int
		next  any
	}{
		{2, 1, float64(3)},
		{3, 2, float64(7)},
		{6, 2, float64(7)},
		{7, 3, float64(14)},
		{13, 3, float64(14)},
		{14, 4, float64(21)},
		{20, 4, float64(21)},
		{21, 5, nil},
	}
	have := 0
	start := streaktime.Today()
	for _, step := range steps {
		for have < step.days {
			have++
			day := start.AddDate(0, 0, -(have - 1))
			images := `[]`
			if have%2 == 0 {
				images = `["checks/p.jpg"]`
			}
			insertMascotCheck(t, db, userID, day, images, day)
		}
		wantUp := step.level > 1
		assertMascotGlow(t, app, step.level, step.days, step.next, wantUp)
	}

	var row domain.UserMascot
	if err := db.Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.GlowLevel != 5 {
		t.Fatalf("stored %d", row.GlowLevel)
	}

	status, env := mascotCall(t, app, http.MethodPost, "/api/v1/me/mascot/glow/seen", "")
	if status != http.StatusOK {
		t.Fatalf("seen %d %#v", status, env)
	}
	assertMascotGlow(t, app, 5, 21, nil, false)
	status, env = mascotCall(t, app, http.MethodPost, "/api/v1/me/mascot/glow/seen", "")
	if status != http.StatusOK {
		t.Fatalf("seen again %d %#v", status, env)
	}
	assertMascotGlow(t, app, 5, 21, nil, false)
	if err := db.Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.GlowLevelSeen != 5 || row.GlowLevel != 5 {
		t.Fatalf("after ack %+v", row)
	}

	if err := db.Where("user_id = ?", userID).Delete(&domain.SkinCheck{}).Error; err != nil {
		t.Fatal(err)
	}
	assertMascotGlow(t, app, 5, 0, nil, false)
	if err := db.Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.GlowLevel != 5 {
		t.Fatalf("stored fell to %d", row.GlowLevel)
	}

	// A stored level above the recomputed one is what GET returns.
	if err := db.Model(&domain.UserMascot{}).Where("user_id = ?", userID).Update("glow_level", 4).Error; err != nil {
		t.Fatal(err)
	}
	assertMascotGlow(t, app, 4, 0, float64(21), false)
	if err := db.Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.GlowLevel != 4 {
		t.Fatalf("read wrote level down to %d", row.GlowLevel)
	}
}

func TestMascotGlowUnlocksAccessories(t *testing.T) {
	head := domain.MascotItemAccessoryPinkHeadband
	star := domain.MascotItemAccessoryStarClip
	base := `{"body":"body_chibi","hair":"hair_sleep_buns","outfit":"outfit_cozy_pajamas","room_items":["room_plant"]}`

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

		seedMascotCheckDays(t, db, userID, 7)
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

		seedMascotCheckDays(t, db, userID, 14)
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
		if data["glow_level"] != float64(4) || data["checkin_days"] != float64(14) || data["next_level_at"] != float64(21) {
			t.Fatalf("glow %#v", data)
		}
	})

	t.Run("default loadout shows them as they unlock", func(t *testing.T) {
		app, db, userID := newMascotApp(t)
		seedMascotCheckDays(t, db, userID, 14)
		status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
		if status != http.StatusOK {
			t.Fatal(status)
		}
		acc := mascotAccessories(t, env)
		if len(acc) != 2 || acc[0] != head || acc[1] != star {
			t.Fatalf("level 4 default %#v", acc)
		}
		data := mascotData(t, env)
		if data["glow_level"] != float64(4) || data["checkin_days"] != float64(14) || data["level_up"] != true {
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
