package adminactivity

import (
	"context"
	"encoding/json"
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

func setupActivityTest(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:admin_activity_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.SkinCheck{}, &domain.RoutineEntry{}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repository.NewSkinCheckRepository(db), repository.NewRoutineEntryRepository(db))
	return svc, db
}

func TestForDate_CheckInPhotoAndProductTick(t *testing.T) {
	svc, db := setupActivityTest(t)
	ctx := context.Background()
	today := streaktime.Today()

	photoUser := &domain.User{Email: "photo@dadiary.test", Username: "photo_user", IsActive: true}
	tickUser := &domain.User{Email: "tick@dadiary.test", Username: "tick_user", IsActive: true}
	idleUser := &domain.User{Email: "idle@dadiary.test", Username: "idle_user", IsActive: true}
	for _, u := range []*domain.User{photoUser, tickUser, idleUser} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}

	imgJSON, _ := json.Marshal([]string{"2026/08/29/check-in/photo_user__x/a.jpg"})
	if err := db.Create(&domain.SkinCheck{
		UserID:    photoUser.ID,
		ImageURLs: imgJSON,
		CheckDate: today,
	}).Error; err != nil {
		t.Fatal(err)
	}

	morning, _ := json.Marshal([]map[string]any{
		{"id": "1", "title": "Cleanser", "completed": true},
		{"id": "2", "title": "SPF", "completed": false},
	})
	if err := db.Create(&domain.RoutineEntry{
		UserID:      tickUser.ID,
		RoutineDate: today,
		Morning:     morning,
		Source:      "manual",
	}).Error; err != nil {
		t.Fatal(err)
	}

	unticked, _ := json.Marshal([]map[string]any{
		{"id": "1", "title": "Cleanser", "completed": false},
	})
	if err := db.Create(&domain.RoutineEntry{
		UserID:      idleUser.ID,
		RoutineDate: today,
		Morning:     unticked,
		Source:      "manual",
	}).Error; err != nil {
		t.Fatal(err)
	}

	out, err := svc.ForDate(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Date != today.Format("2006-01-02") {
		t.Fatalf("date=%s", out.Date)
	}
	if out.CheckInCount != 1 || out.CheckInPhotoCount != 1 {
		t.Fatalf("check-ins=%d photos=%d", out.CheckInCount, out.CheckInPhotoCount)
	}
	if out.CheckIns[0].Username != "photo_user" || !out.CheckIns[0].HasPhotos {
		t.Fatalf("check-in=%+v", out.CheckIns[0])
	}
	if out.ProductUsageCount != 1 {
		t.Fatalf("product usage=%d", out.ProductUsageCount)
	}
	if out.ProductUsage[0].Username != "tick_user" || out.ProductUsage[0].MorningTicked != 1 {
		t.Fatalf("usage=%+v", out.ProductUsage[0])
	}

	yesterday := today.AddDate(0, 0, -1).Format("2006-01-02")
	empty, err := svc.ForDate(ctx, yesterday)
	if err != nil {
		t.Fatal(err)
	}
	if empty.CheckInCount != 0 || empty.ProductUsageCount != 0 {
		t.Fatalf("yesterday should be empty: %+v", empty)
	}
}

func TestForDate_InvalidDate(t *testing.T) {
	svc, _ := setupActivityTest(t)
	_, err := svc.ForDate(context.Background(), "29-08-2026")
	if err == nil {
		t.Fatal("expected invalid date")
	}
}

func TestParseActivityDate_UsesVietnamDay(t *testing.T) {
	got, err := parseActivityDate("2026-08-29")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
