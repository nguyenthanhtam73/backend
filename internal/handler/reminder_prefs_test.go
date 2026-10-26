package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	reminderprefsuc "github.com/dadiary/backend/internal/usecase/reminderprefs"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newReminderPrefsApp(t *testing.T) (*fiber.App, *repository.GormUserRepository, uuid.UUID) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:reminder_http_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	u := &domain.User{Email: "prefs-http@test.com", Username: "prefs-http@test.com", IsActive: true}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	h := NewReminderPrefsHandler(reminderprefsuc.NewService(users))
	app := fiber.New()
	auth := func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalsUserID, u.ID)
		return c.Next()
	}
	app.Get("/api/v1/me/reminder", auth, h.Get)
	app.Put("/api/v1/me/reminder", auth, h.Put)
	return app, users, u.ID
}

func putReminder(t *testing.T, app *fiber.App, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/reminder", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
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

func getReminder(t *testing.T, app *fiber.App) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/reminder", nil)
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

func reminderData(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("data: %#v", env)
	}
	return data
}

func errorCode(t *testing.T, env map[string]any) string {
	t.Helper()
	errBody, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("error: %#v", env)
	}
	code, _ := errBody["code"].(string)
	return code
}

func TestReminderPrefs_OldBodyAndUnsetSchedule(t *testing.T) {
	app, _, _ := newReminderPrefsApp(t)

	status, env := getReminder(t, app)
	if status != http.StatusOK {
		t.Fatalf("get status=%d env=%v", status, env)
	}
	data := reminderData(t, env)
	if _, ok := data["schedule"]; ok {
		t.Fatalf("schedule present before any save: %#v", data)
	}
	if _, ok := data["push_opt_in_skipped_at"]; ok {
		t.Fatalf("skipped_at present before a skip: %#v", data)
	}
	if data["push_opt_in_reshow_eligible"] != false || data["push_opt_in_show_after_check_in_only"] != true {
		t.Fatalf("old fields changed: %#v", data)
	}
	if len(data) != 2 {
		t.Fatalf("unset GET keys = %#v", data)
	}

	status, env = putReminder(t, app, `{"push_opt_in_action":"skip_push_opt_in"}`)
	if status != http.StatusOK {
		t.Fatalf("put status=%d env=%v", status, env)
	}
	data = reminderData(t, env)
	if _, ok := data["schedule"]; ok {
		t.Fatalf("old body wrote a schedule: %#v", data)
	}
	skipped, ok := data["push_opt_in_skipped_at"].(string)
	if !ok || skipped == "" {
		t.Fatalf("skip did not set push_opt_in_skipped_at: %#v", data)
	}
	if _, err := time.Parse(time.RFC3339, skipped); err != nil {
		t.Fatalf("skipped_at %q: %v", skipped, err)
	}
	if data["push_opt_in_reshow_eligible"] != false || data["push_opt_in_show_after_check_in_only"] != true {
		t.Fatalf("old fields changed after skip: %#v", data)
	}

	status, env = getReminder(t, app)
	if status != http.StatusOK {
		t.Fatalf("get after skip status=%d", status)
	}
	data = reminderData(t, env)
	if _, ok := data["schedule"]; ok {
		t.Fatalf("GET grew a schedule: %#v", data)
	}
	if data["push_opt_in_skipped_at"] == nil {
		t.Fatalf("GET lost skipped_at: %#v", data)
	}
}

func TestReminderPrefs_EmptyBodyAndInvalidJSON(t *testing.T) {
	app, _, _ := newReminderPrefsApp(t)
	status, env := putReminder(t, app, `{}`)
	if status != http.StatusBadRequest || errorCode(t, env) != "invalid_action" {
		t.Fatalf("empty body status=%d env=%v", status, env)
	}
	status, env = putReminder(t, app, `{`)
	if status != http.StatusBadRequest || errorCode(t, env) != "invalid_json" {
		t.Fatalf("bad json status=%d env=%v", status, env)
	}
}

func TestReminderPrefs_ScheduleValidationAndDefaultTimezone(t *testing.T) {
	app, _, _ := newReminderPrefsApp(t)

	status, env := putReminder(t, app, `{"enabled":true}`)
	if status != http.StatusOK {
		t.Fatalf("default tz status=%d env=%v", status, env)
	}
	schedule := scheduleOf(t, reminderData(t, env))
	if schedule["enabled"] != true {
		t.Fatalf("enabled: %#v", schedule)
	}
	if schedule["time"] != nil {
		t.Fatalf("time should stay unset: %#v", schedule)
	}
	if schedule["timezone"] != "Asia/Ho_Chi_Minh" {
		t.Fatalf("timezone default: %#v", schedule)
	}
	if reminderData(t, env)["push_opt_in_show_after_check_in_only"] != true {
		t.Fatalf("old field missing after schedule write: %#v", env)
	}

	status, env = putReminder(t, app, `{"time":"25:00"}`)
	if status != http.StatusBadRequest || errorCode(t, env) != "invalid_time" {
		t.Fatalf("bad time status=%d env=%v", status, env)
	}
	status, env = getReminder(t, app)
	if scheduleOf(t, reminderData(t, env))["timezone"] != "Asia/Ho_Chi_Minh" || scheduleOf(t, reminderData(t, env))["enabled"] != true {
		t.Fatalf("bad time wrote a schedule: %#v", env)
	}

	status, env = putReminder(t, app, `{"timezone":"Mars/Phobos"}`)
	if status != http.StatusBadRequest || errorCode(t, env) != "invalid_timezone" {
		t.Fatalf("bad zone status=%d env=%v", status, env)
	}
	status, env = putReminder(t, app, `{"time":"9:00"}`)
	if status != http.StatusBadRequest || errorCode(t, env) != "invalid_time" {
		t.Fatalf("short time status=%d env=%v", status, env)
	}

	status, env = putReminder(t, app, `{"enabled":false,"time":" 21:05 ","timezone":"America/Los_Angeles"}`)
	if status != http.StatusOK {
		t.Fatalf("la status=%d env=%v", status, env)
	}
	schedule = scheduleOf(t, reminderData(t, env))
	if schedule["enabled"] != false || schedule["time"] != "21:05" || schedule["timezone"] != "America/Los_Angeles" {
		t.Fatalf("la schedule: %#v", schedule)
	}

	status, env = putReminder(t, app, `{"time":"08:00","timezone":""}`)
	if status != http.StatusOK {
		t.Fatalf("blank tz status=%d env=%v", status, env)
	}
	schedule = scheduleOf(t, reminderData(t, env))
	if schedule["enabled"] != false || schedule["time"] != "08:00" || schedule["timezone"] != "America/Los_Angeles" {
		t.Fatalf("blank timezone should keep America/Los_Angeles and enabled false: %#v", schedule)
	}

	status, env = putReminder(t, app, `{"time":"00:00","timezone":"UTC"}`)
	if status != http.StatusOK {
		t.Fatalf("utc status=%d env=%v", status, env)
	}
	status, env = putReminder(t, app, `{"time":"23:59","timezone":"Asia/Ho_Chi_Minh"}`)
	if status != http.StatusOK {
		t.Fatalf("2359 status=%d env=%v", status, env)
	}
	schedule = scheduleOf(t, reminderData(t, env))
	if schedule["time"] != "23:59" || schedule["timezone"] != "Asia/Ho_Chi_Minh" {
		t.Fatalf("edge time: %#v", schedule)
	}
}

func TestReminderPrefs_ActionAndScheduleTogether(t *testing.T) {
	app, _, _ := newReminderPrefsApp(t)
	status, env := putReminder(t, app, `{"push_opt_in_action":"nope","enabled":true}`)
	if status != http.StatusBadRequest || errorCode(t, env) != "invalid_action" {
		t.Fatalf("bad action status=%d env=%v", status, env)
	}
	status, env = getReminder(t, app)
	if _, ok := reminderData(t, env)["schedule"]; ok {
		t.Fatalf("rejected action still saved a schedule: %#v", env)
	}

	status, env = putReminder(t, app, `{"push_opt_in_action":"skip_push_opt_in","enabled":true,"time":"20:30"}`)
	if status != http.StatusOK {
		t.Fatalf("both status=%d env=%v", status, env)
	}
	data := reminderData(t, env)
	if data["push_opt_in_skipped_at"] == nil {
		t.Fatalf("action not applied: %#v", data)
	}
	schedule := scheduleOf(t, data)
	if schedule["enabled"] != true || schedule["time"] != "20:30" || schedule["timezone"] != "Asia/Ho_Chi_Minh" {
		t.Fatalf("schedule not applied: %#v", schedule)
	}
}

func TestReminderPrefs_OmittedTimezoneKeepsStored(t *testing.T) {
	app, _, _ := newReminderPrefsApp(t)

	status, env := putReminder(t, app, `{"enabled":true,"time":"21:30","timezone":"Asia/Dubai"}`)
	if status != http.StatusOK {
		t.Fatalf("dubai status=%d env=%v", status, env)
	}

	status, env = putReminder(t, app, `{"enabled":false}`)
	if status != http.StatusOK {
		t.Fatalf("disable status=%d env=%v", status, env)
	}
	schedule := scheduleOf(t, reminderData(t, env))
	if schedule["enabled"] != false || schedule["time"] != "21:30" || schedule["timezone"] != "Asia/Dubai" {
		t.Fatalf("omit timezone overwrote the stored schedule: %#v", schedule)
	}

	status, env = putReminder(t, app, `{"timezone":""}`)
	if status != http.StatusOK {
		t.Fatalf("blank tz status=%d env=%v", status, env)
	}
	schedule = scheduleOf(t, reminderData(t, env))
	if schedule["enabled"] != false || schedule["time"] != "21:30" || schedule["timezone"] != "Asia/Dubai" {
		t.Fatalf("empty timezone overwrote the stored schedule: %#v", schedule)
	}

	status, env = putReminder(t, app, `{"time":""}`)
	if status != http.StatusOK {
		t.Fatalf("blank time status=%d env=%v", status, env)
	}
	schedule = scheduleOf(t, reminderData(t, env))
	if schedule["time"] != "21:30" || schedule["timezone"] != "Asia/Dubai" {
		t.Fatalf("empty time cleared the stored clock: %#v", schedule)
	}
}

func TestReminderPrefs_FirstScheduleDefaultsTimezone(t *testing.T) {
	app, _, _ := newReminderPrefsApp(t)
	status, env := putReminder(t, app, `{"enabled":false}`)
	if status != http.StatusOK {
		t.Fatalf("status=%d env=%v", status, env)
	}
	schedule := scheduleOf(t, reminderData(t, env))
	if schedule["enabled"] != false || schedule["timezone"] != "Asia/Ho_Chi_Minh" || schedule["time"] != nil {
		t.Fatalf("first schedule: %#v", schedule)
	}
}

func scheduleOf(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	schedule, ok := data["schedule"].(map[string]any)
	if !ok {
		t.Fatalf("schedule: %#v", data)
	}
	return schedule
}
