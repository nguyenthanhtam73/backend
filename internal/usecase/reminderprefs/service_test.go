package reminderprefs

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReshowEligible_ThreeVietnamDaysAfterSkip(t *testing.T) {
	skip := time.Date(2026, 10, 5, 22, 0, 0, 0, streaktime.Location)
	if ReshowEligible(&skip, nil, time.Date(2026, 10, 7, 23, 0, 0, 0, streaktime.Location)) {
		t.Fatal("day 2 after skip is too soon")
	}
	if !ReshowEligible(&skip, nil, time.Date(2026, 10, 8, 0, 5, 0, 0, streaktime.Location)) {
		t.Fatal("day 3 after skip should be eligible")
	}
	used := skip.Add(72 * time.Hour)
	if ReshowEligible(&skip, &used, time.Date(2026, 10, 8, 12, 0, 0, 0, streaktime.Location)) {
		t.Fatal("consumed re-show must not be eligible")
	}
	if ReshowEligible(nil, nil, time.Now()) {
		t.Fatal("never skipped is not a re-show")
	}
}

func TestApply_SkipOnceThenConsume(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:prefs_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	u := &domain.User{Email: "prefs@test.com", Username: "prefs@test.com", IsActive: true}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	svc := NewService(users)
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, streaktime.Location)
	svc.now = func() time.Time { return now }

	view, err := svc.Get(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.PushOptInReshowEligible || !view.PushOptInShowAfterCheckInOnly {
		t.Fatalf("initial: %+v", view)
	}

	view, err = svc.Apply(context.Background(), u.ID, Update{Action: ActionSkip})
	if err != nil {
		t.Fatal(err)
	}
	if view.PushOptInSkippedAt == nil || view.PushOptInReshowEligible {
		t.Fatalf("just skipped: %+v", view)
	}
	firstSkip := *view.PushOptInSkippedAt

	later := now.Add(time.Hour)
	svc.now = func() time.Time { return later }
	if _, err := svc.Apply(context.Background(), u.ID, Update{Action: ActionSkip}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := users.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.PushOptInSkippedAt == nil || !reloaded.PushOptInSkippedAt.Equal(firstSkip) {
		t.Fatalf("skip clock moved: %v want %v", reloaded.PushOptInSkippedAt, firstSkip)
	}

	svc.now = func() time.Time { return time.Date(2026, 10, 8, 19, 0, 0, 0, streaktime.Location) }
	view, err = svc.Get(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.PushOptInReshowEligible {
		t.Fatalf("expected re-show: %+v", view)
	}
	view, err = svc.Apply(context.Background(), u.ID, Update{Action: ActionConsumeReshow})
	if err != nil {
		t.Fatal(err)
	}
	if view.PushOptInReshowEligible {
		t.Fatalf("consumed still eligible: %+v", view)
	}
	if _, err := svc.Apply(context.Background(), u.ID, Update{Action: "nope"}); err != ErrInvalidAction {
		t.Fatalf("err=%v", err)
	}
}

func TestApply_OmitsTimezoneKeepsStored(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:prefs_tz_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	u := &domain.User{Email: "dubai@test.com", Username: "dubai@test.com", IsActive: true}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	svc := NewService(users)
	enabled := true
	hhmm := "21:30"
	tz := "Asia/Dubai"
	view, err := svc.Apply(context.Background(), u.ID, Update{Enabled: &enabled, Time: &hhmm, Timezone: &tz})
	if err != nil {
		t.Fatal(err)
	}
	if view.Schedule == nil || view.Schedule.Timezone == nil || *view.Schedule.Timezone != "Asia/Dubai" {
		t.Fatalf("store dubai: %+v", view.Schedule)
	}

	off := false
	view, err = svc.Apply(context.Background(), u.ID, Update{Enabled: &off})
	if err != nil {
		t.Fatal(err)
	}
	if view.Schedule == nil || view.Schedule.Enabled == nil || *view.Schedule.Enabled {
		t.Fatalf("enabled: %+v", view.Schedule)
	}
	if view.Schedule.Timezone == nil || *view.Schedule.Timezone != "Asia/Dubai" {
		t.Fatalf("timezone: %+v", view.Schedule)
	}
	if view.Schedule.Time == nil || *view.Schedule.Time != "21:30" {
		t.Fatalf("time: %+v", view.Schedule)
	}

	blankTZ := "  "
	view, err = svc.Apply(context.Background(), u.ID, Update{Timezone: &blankTZ})
	if err != nil {
		t.Fatal(err)
	}
	if view.Schedule.Timezone == nil || *view.Schedule.Timezone != "Asia/Dubai" || view.Schedule.Time == nil || *view.Schedule.Time != "21:30" {
		t.Fatalf("blank timezone: %+v", view.Schedule)
	}

	blankTime := ""
	view, err = svc.Apply(context.Background(), u.ID, Update{Time: &blankTime})
	if err != nil {
		t.Fatal(err)
	}
	if view.Schedule.Time == nil || *view.Schedule.Time != "21:30" || view.Schedule.Timezone == nil || *view.Schedule.Timezone != "Asia/Dubai" {
		t.Fatalf("blank time: %+v", view.Schedule)
	}
}

func TestApply_FirstScheduleDefaultsTimezone(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:prefs_tz0_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	u := &domain.User{Email: "first@test.com", Username: "first@test.com", IsActive: true}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	svc := NewService(users)
	off := false
	view, err := svc.Apply(context.Background(), u.ID, Update{Enabled: &off})
	if err != nil {
		t.Fatal(err)
	}
	if view.Schedule == nil || view.Schedule.Timezone == nil || *view.Schedule.Timezone != DefaultReminderTimezone {
		t.Fatalf("default timezone: %+v", view.Schedule)
	}
	if view.Schedule.Time != nil {
		t.Fatalf("time was stored: %+v", view.Schedule)
	}
	if view.Schedule.Enabled == nil || *view.Schedule.Enabled {
		t.Fatalf("enabled: %+v", view.Schedule)
	}
}

func TestApply_ConsumeBeforeSkipDoesNotBurnReshow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:prefs2_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	u := &domain.User{Email: "early@test.com", Username: "early@test.com", IsActive: true}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	svc := NewService(users)
	svc.now = func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, streaktime.Location) }
	if _, err := svc.Apply(context.Background(), u.ID, Update{Action: ActionConsumeReshow}); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Date(2026, 10, 5, 9, 0, 0, 0, streaktime.Location) }
	if _, err := svc.Apply(context.Background(), u.ID, Update{Action: ActionSkip}); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Date(2026, 10, 8, 9, 0, 0, 0, streaktime.Location) }
	view, err := svc.Get(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.PushOptInReshowEligible {
		t.Fatalf("early consume burned the re-show: %+v", view)
	}
}
