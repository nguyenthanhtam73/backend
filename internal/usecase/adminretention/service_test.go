package adminretention

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openRetentionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:admin_retention_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.SkinCheck{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func isoWeekLabel(day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		panic(err)
	}
	year, week := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

var checkImages = []byte(`["a.jpg"]`)

func mustUser(t *testing.T, db *gorm.DB, email, username string, created time.Time) uuid.UUID {
	t.Helper()
	u := &domain.User{Email: email, Username: username, IsActive: true, CreatedAt: created}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.NewUserRepository(db).SetCreatedAtForTest(context.Background(), u.ID, created); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func addInstant(t *testing.T, db *gorm.DB, userID uuid.UUID, at time.Time) uuid.UUID {
	t.Helper()
	row := &domain.SkinCheck{
		UserID:    userID,
		ImageURLs: checkImages,
		CheckDate: streaktime.DateOf(at),
		CreatedAt: at,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	return row.ID
}

func addCivilDay(t *testing.T, db *gorm.DB, userID uuid.UUID, day string) uuid.UUID {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatal(err)
	}
	// 05:00 UTC is 12:00 in Vietnam, so the civil day matches `day`.
	at := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 5, 0, 0, 0, time.UTC)
	if got := streaktime.DateOf(at).Format("2006-01-02"); got != day {
		t.Fatalf("civil day helper %s -> %s", day, got)
	}
	return addInstant(t, db, userID, at)
}

func addRun(t *testing.T, db *gorm.DB, userID uuid.UUID, start string, n int) {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", start)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		addCivilDay(t, db, userID, parsed.AddDate(0, 0, i).Format("2006-01-02"))
	}
}

func TestSQLiteISOWeekMatchesGo(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:iso_week_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	days := []string{
		"2020-01-01",
		"2024-12-30",
		"2025-12-29",
		"2026-01-01",
		"2026-06-15",
		"2026-07-01",
		"2026-08-01", // Saturday
		"2026-08-02", // Sunday
		"2026-08-06", // Thursday, offset 0
		"2026-09-02",
		"2026-09-07",
		"2026-09-08",
		"2026-12-31",
	}
	for _, day := range days {
		var got string
		q := "SELECT " + sqliteISOWeekSQL("'"+day+"'")
		if err := db.Raw(q).Scan(&got).Error; err != nil {
			t.Fatalf("%s: %v", day, err)
		}
		if got != isoWeekLabel(day) {
			t.Fatalf("%s iso week %s want %s", day, got, isoWeekLabel(day))
		}
	}
}

func TestQueriesAreSelectOnly(t *testing.T) {
	where := "u.deleted_at IS NULL AND u.is_active = ?"
	keyword := regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE|CREATE)\b`)
	for _, d := range []dialect{sqliteDialect(), postgresDialect()} {
		for _, q := range []string{d.summarySQL(where), d.cohortSQL(where)} {
			if !strings.Contains(strings.ToUpper(q), "SELECT") {
				t.Fatalf("%s query missing SELECT", d.name)
			}
			if m := keyword.FindString(q); m != "" {
				t.Fatalf("%s query contains %s\n%s", d.name, m, q)
			}
		}
	}
}

func TestParseDayRange(t *testing.T) {
	from, to, err := ParseDayRange(" 2026-09-02 ", "")
	if err != nil || from == nil || *from != "2026-09-02" || to != nil {
		t.Fatalf("from=%v to=%v err=%v", from, to, err)
	}
	if _, _, err := ParseDayRange("2026-09-08", "2026-09-02"); err == nil {
		t.Fatal("expected from after to to fail")
	}
	for _, raw := range []string{"2026-9-02", "2026-02-31", "09-02-2026", "today"} {
		if _, _, err := ParseDayRange(raw, ""); err == nil {
			t.Fatalf("expected %q to fail", raw)
		}
	}
}

func TestStats_Unavailable(t *testing.T) {
	var svc *Service
	if _, err := svc.Stats(context.Background(), Query{}); err != ErrUnavailable {
		t.Fatalf("nil service err=%v", err)
	}
	svc = NewService(nil)
	if _, err := svc.Stats(context.Background(), Query{}); err != ErrUnavailable {
		t.Fatalf("nil db err=%v", err)
	}
}

func TestStats_BucketsExclusionsAndMidnightBoundary(t *testing.T) {
	exerciseRetention(t, openRetentionDB(t))
}

// TestStats_BucketsOnPostgres runs the same fixture against Postgres.
// The session time zone is America/Los_Angeles so a query that used the
// session zone instead of Asia/Ho_Chi_Minh would fail the midnight cases.
func TestStats_BucketsOnPostgres(t *testing.T) {
	dsn := os.Getenv("DADIARY_RETENTION_TEST_DSN")
	if dsn == "" {
		t.Skip("set DADIARY_RETENTION_TEST_DSN to exercise retention SQL on Postgres")
	}
	if !strings.Contains(strings.ToLower(dsn), "test") {
		t.Fatal("DADIARY_RETENTION_TEST_DSN must point at a throwaway database whose name contains \"test\"")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("SET TIME ZONE 'America/Los_Angeles'").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TABLE IF EXISTS skin_checks").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TABLE IF EXISTS users").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.SkinCheck{}); err != nil {
		t.Fatal(err)
	}
	exerciseRetention(t, db)
}

func exerciseRetention(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	svc := NewService(db)

	// 16:30 UTC 1 Sep is still 23:30 in Vietnam. 17:30 UTC is 00:30 the next VN day.
	early := time.Date(2026, 9, 1, 16, 30, 0, 0, time.UTC)
	late := time.Date(2026, 9, 1, 17, 30, 0, 0, time.UTC)
	if early.Format("2006-01-02") != late.Format("2006-01-02") {
		t.Fatal("fixture instants must share a UTC date")
	}
	if streaktime.DateOf(early).Format("2006-01-02") != "2026-09-01" || streaktime.DateOf(late).Format("2006-01-02") != "2026-09-02" {
		t.Fatalf("VN days early=%s late=%s", streaktime.DateOf(early), streaktime.DateOf(late))
	}

	boundary := mustUser(t, db, "boundary@dadiary.test", "boundary", late)
	addInstant(t, db, boundary, early)
	addInstant(t, db, boundary, late)

	onceAt := time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC) // 10:00 VN, same civil day
	once := mustUser(t, db, "once@dadiary.test", "once", onceAt)
	addCivilDay(t, db, once, "2026-09-07")

	fourteen := mustUser(t, db, "fourteen@dadiary.test", "fourteen", time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC))
	addRun(t, db, fourteen, "2026-07-25", 14) // 25 Jul–7 Aug, crosses the month boundary
	addInstant(t, db, fourteen, time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC))
	deletedID := addCivilDay(t, db, fourteen, "2026-08-08")
	if err := db.Delete(&domain.SkinCheck{}, "id = ?", deletedID).Error; err != nil {
		t.Fatal(err)
	}

	gap := mustUser(t, db, "gap@dadiary.test", "gap", time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC))
	for _, day := range []string{"2026-08-20", "2026-08-22", "2026-08-23", "2026-08-24", "2026-08-25", "2026-08-26", "2026-08-27", "2026-08-28"} {
		addCivilDay(t, db, gap, day)
	}

	broken := mustUser(t, db, "broken@dadiary.test", "broken", time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC))
	for _, day := range []string{"2026-07-01", "2026-07-02", "2026-07-03", "2026-07-05", "2026-07-06", "2026-07-08", "2026-07-09"} {
		addCivilDay(t, db, broken, day)
	}

	// Registered, never checked in. 20:00 UTC 7 Sep is 03:00 VN on 8 Sep.
	idleAt := time.Date(2026, 9, 7, 20, 0, 0, 0, time.UTC)
	if streaktime.DateOf(idleAt).Format("2006-01-02") != "2026-09-08" {
		t.Fatalf("idle VN day %s", streaktime.DateOf(idleAt))
	}
	mustUser(t, db, "idle@dadiary.test", "idle", idleAt)

	ghost := mustUser(t, db, "ghost@dadiary.test", "ghost", idleAt)
	ghostCheck := addCivilDay(t, db, ghost, "2026-09-08")
	if err := db.Delete(&domain.SkinCheck{}, "id = ?", ghostCheck).Error; err != nil {
		t.Fatal(err)
	}

	// Plus-tag is required; this address must stay in the counts.
	mustUser(t, db, "goalacne1003@dadiary.test", "lookalike", time.Date(2026, 6, 15, 2, 0, 0, 0, time.UTC))

	// Each excluded account has a 20-day run. A leak would move max_days to 20.
	exclude := []struct {
		email, username string
		inactive        bool
		deleted         bool
	}{
		{email: "qa+goalacne1003@dadiary.test", username: "tag_goal"},
		{email: "QA+NoLabel1003@dadiary.test", username: "tag_nolabel"},
		{email: "qa+fe53test1003@dadiary.test", username: "tag_fe"},
		{email: "qa+dadiarytest0925@dadiary.test", username: "tag_diary"},
		{email: "Founder@DaDiary.vn", username: "founder"},
		{email: "reviewer@dadiary.vn", username: "reviewer"},
		{email: "paused@dadiary.test", username: "paused", inactive: true},
		{email: "gone@dadiary.test", username: "gone", deleted: true},
	}
	may := time.Date(2026, 5, 1, 2, 0, 0, 0, time.UTC)
	for _, ex := range exclude {
		id := mustUser(t, db, ex.email, ex.username, may)
		addRun(t, db, id, "2026-05-01", 20)
		if ex.inactive {
			if err := db.Exec("UPDATE users SET is_active = ? WHERE id = ?", false, id).Error; err != nil {
				t.Fatal(err)
			}
		}
		if ex.deleted {
			if err := db.Delete(&domain.User{}, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
		}
	}

	var usersBefore, checksBefore int64
	if err := db.Model(&domain.User{}).Count(&usersBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.SkinCheck{}).Count(&checksBefore).Error; err != nil {
		t.Fatal(err)
	}

	out, err := svc.Stats(ctx, Query{
		ExcludedEmails: []string{"founder@dadiary.vn", "reviewer@dadiary.vn"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var usersAfter, checksAfter int64
	if err := db.Model(&domain.User{}).Count(&usersAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.SkinCheck{}).Count(&checksAfter).Error; err != nil {
		t.Fatal(err)
	}
	if usersBefore != usersAfter || checksBefore != checksAfter {
		t.Fatalf("stats wrote rows users %d→%d checks %d→%d", usersBefore, usersAfter, checksBefore, checksAfter)
	}

	if out.RegisteredUsers != 8 {
		t.Fatalf("registered_users=%d want 8", out.RegisteredUsers)
	}
	if out.UsersWithCheckin != 5 {
		t.Fatalf("users_with_checkin=%d want 5", out.UsersWithCheckin)
	}
	if out.DaysUsed.AtLeast1 != 5 || out.DaysUsed.AtLeast2 != 4 || out.DaysUsed.AtLeast7 != 3 || out.DaysUsed.AtLeast14 != 1 || out.DaysUsed.MaxDays != 14 {
		t.Fatalf("days_used=%+v", out.DaysUsed)
	}
	if out.Consecutive.AtLeast1 != 5 || out.Consecutive.AtLeast2 != 4 || out.Consecutive.AtLeast7 != 2 || out.Consecutive.AtLeast14 != 1 {
		t.Fatalf("consecutive=%+v", out.Consecutive)
	}
	if out.ReturnNextDay != 3 {
		t.Fatalf("return_next_day=%d want 3", out.ReturnNextDay)
	}
	if out.Calendar != "Asia/Ho_Chi_Minh" || out.From != nil || out.To != nil {
		t.Fatalf("meta from=%v to=%v calendar=%s", out.From, out.To, out.Calendar)
	}
	if _, err := time.Parse(time.RFC3339, out.AsOf); err != nil {
		t.Fatalf("as_of %q: %v", out.AsOf, err)
	}

	wantWeeks := []struct {
		day                                     string
		registered, checked, atLeast2, returned int64
	}{
		{"2026-06-15", 1, 0, 0, 0},
		{"2026-07-01", 1, 1, 1, 1},
		{"2026-08-01", 1, 1, 1, 1},
		{"2026-08-20", 1, 1, 1, 0},
		{"2026-09-02", 1, 1, 1, 1},
		// 7 Sep and 8 Sep share ISO week W37: once checked in, idle and ghost did not.
		{"2026-09-07", 3, 1, 0, 0},
	}
	if len(out.BySignupWeek) != len(wantWeeks) {
		t.Fatalf("weeks=%d %+v", len(out.BySignupWeek), out.BySignupWeek)
	}
	for i, want := range wantWeeks {
		got := out.BySignupWeek[i]
		label := isoWeekLabel(want.day)
		if got.Week != label || got.Registered != want.registered || got.CheckedInOnce != want.checked || got.AtLeast2Days != want.atLeast2 || got.ReturnedNextDay != want.returned {
			t.Fatalf("week %s got %+v want %s reg=%d checked=%d two=%d back=%d", want.day, got, label, want.registered, want.checked, want.atLeast2, want.returned)
		}
	}

	// Registration filter uses the Vietnam civil day. `late` is 1 Sep UTC and 2 Sep VN.
	from, to := "2026-09-02", "2026-09-02"
	day, err := svc.Stats(ctx, Query{From: &from, To: &to, ExcludedEmails: []string{"founder@dadiary.vn", "reviewer@dadiary.vn"}})
	if err != nil {
		t.Fatal(err)
	}
	if day.RegisteredUsers != 1 || day.UsersWithCheckin != 1 || day.ReturnNextDay != 1 {
		t.Fatalf("single day filter %+v", day)
	}
	if day.DaysUsed.AtLeast1 != 1 || day.DaysUsed.AtLeast2 != 1 || day.DaysUsed.AtLeast7 != 0 || day.DaysUsed.MaxDays != 2 {
		t.Fatalf("single day days_used %+v", day.DaysUsed)
	}
	if day.Consecutive.AtLeast1 != 1 || day.Consecutive.AtLeast2 != 1 || day.Consecutive.AtLeast7 != 0 || day.Consecutive.AtLeast14 != 0 {
		t.Fatalf("single day consecutive %+v", day.Consecutive)
	}
	if day.From == nil || *day.From != from || day.To == nil || *day.To != to {
		t.Fatalf("echo from=%v to=%v", day.From, day.To)
	}
	if len(day.BySignupWeek) != 1 || day.BySignupWeek[0].Week != isoWeekLabel("2026-09-02") || day.BySignupWeek[0].CheckedInOnce != 1 || day.BySignupWeek[0].AtLeast2Days != 1 {
		t.Fatalf("single day weeks %+v", day.BySignupWeek)
	}

	// to=7 Sep includes once (VN 7 Sep) and excludes idle/ghost (VN 8 Sep, UTC 7 Sep).
	upper := "2026-09-07"
	upto, err := svc.Stats(ctx, Query{To: &upper, ExcludedEmails: []string{"founder@dadiary.vn", "reviewer@dadiary.vn"}})
	if err != nil {
		t.Fatal(err)
	}
	if upto.RegisteredUsers != 6 || upto.UsersWithCheckin != 5 || upto.DaysUsed.MaxDays != 14 || upto.ReturnNextDay != 3 {
		t.Fatalf("to=%s %+v days=%+v", upper, upto, upto.DaysUsed)
	}
	last := upto.BySignupWeek[len(upto.BySignupWeek)-1]
	if last.Week != isoWeekLabel("2026-09-07") || last.Registered != 1 || last.CheckedInOnce != 1 {
		t.Fatalf("last week %+v", last)
	}

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"registered_users", "users_with_checkin", "days_used", "consecutive", "return_next_day", "by_signup_week", "from", "to", "calendar", "as_of"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing json key %s in %s", key, raw)
		}
	}
	days, _ := payload["days_used"].(map[string]any)
	for _, key := range []string{"at_least_1", "at_least_2", "at_least_7", "at_least_14", "max_days"} {
		if _, ok := days[key]; !ok {
			t.Fatalf("missing days_used.%s", key)
		}
	}
	consec, _ := payload["consecutive"].(map[string]any)
	for _, key := range []string{"at_least_1", "at_least_2", "at_least_7", "at_least_14"} {
		if _, ok := consec[key]; !ok {
			t.Fatalf("missing consecutive.%s", key)
		}
	}
	t.Logf("sample %s", raw)
}
