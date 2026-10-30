package adminstats

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupStatsDB(t *testing.T) (*gorm.DB, *Service) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:admin_stats_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}); err != nil {
		t.Fatal(err)
	}
	return db, NewService(repository.NewUserRepository(db))
}

func strPtr(s string) *string { return &s }

func seedSignup(t *testing.T, db *gorm.DB, email, username string, at time.Time, source, fb, tt, medium *string, inactive bool) *domain.User {
	t.Helper()
	u := &domain.User{
		Email:     email,
		Username:  username,
		IsActive:  true,
		UTMSource: source,
		UTMMedium: medium,
		FBCLID:    fb,
		TTCLID:    tt,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	if inactive {
		if err := db.Model(u).Update("is_active", false).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.NewUserRepository(db).SetCreatedAtForTest(context.Background(), u.ID, at); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestRegistrationsBySource_TimezoneSinceAndExclusions(t *testing.T) {
	db, svc := setupStatsDB(t)
	// 2026-10-30 17:30 UTC == 2026-10-31 00:30 Asia/Ho_Chi_Minh.
	boundary := time.Date(2026, 10, 30, 17, 30, 0, 0, time.UTC)
	// One minute before the Vietnam civil day that contains boundary.
	beforeVNDay := time.Date(2026, 10, 29, 16, 59, 0, 0, time.UTC)
	exactVNStart := time.Date(2026, 10, 30, 17, 0, 0, 0, time.UTC) // 2026-10-31 00:00 VN

	seedSignup(t, db, "boundary@test.com", "boundary", boundary, strPtr("tiktok"), nil, strPtr("tt.boundary"), nil, false)
	seedSignup(t, db, "before@test.com", "before", beforeVNDay, nil, strPtr("fb-before"), nil, nil, false)
	onStart := seedSignup(t, db, "onstart@test.com", "onstart", exactVNStart, nil, nil, nil, strPtr("cpc"), false)
	inactive := seedSignup(t, db, "inactive-marker@test.com", "inactive", boundary, strPtr("newsletter-marker"), nil, nil, nil, true)
	deleted := seedSignup(t, db, "deleted-marker@test.com", "deleted", boundary, nil, nil, strPtr("tt.deleted"), nil, false)
	if err := db.Delete(&domain.User{}, deleted.ID).Error; err != nil {
		t.Fatal(err)
	}
	var still domain.User
	if err := db.First(&still, inactive.ID).Error; err != nil {
		t.Fatal(err)
	}
	if still.IsActive {
		t.Fatal("inactive fixture was stored active")
	}

	vn, err := svc.RegistrationsBySource(context.Background(), "2026-10-31", "Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	if vn.Timezone != "Asia/Ho_Chi_Minh" || vn.Since != "2026-10-31" {
		t.Fatalf("window: %+v", vn)
	}
	if len(vn.Days) != 1 || vn.Days[0].Date != "2026-10-31" {
		t.Fatalf("days: %+v", vn.Days)
	}
	day := vn.Days[0]
	// boundary tiktok + inactive other. onStart is none (medium only).
	// beforeVNDay is the previous civil day. deleted tiktok is gone.
	if day.TikTok != 1 || day.Facebook != 0 || day.Other != 1 || day.None != 1 {
		t.Fatalf("vn counts: %+v", day)
	}

	utc, err := svc.RegistrationsBySource(context.Background(), "2026-10-30", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if utc.Timezone != "UTC" || len(utc.Days) != 1 || utc.Days[0].Date != "2026-10-30" {
		t.Fatalf("utc: %+v", utc)
	}
	// exactVNStart is 2026-10-30 17:00 UTC, same UTC day as boundary.
	// beforeVNDay is 2026-10-29 UTC, outside since=2026-10-30.
	if utc.Days[0].TikTok != 1 || utc.Days[0].Other != 1 || utc.Days[0].None != 1 || utc.Days[0].Facebook != 0 {
		t.Fatalf("utc counts: %+v", utc.Days[0])
	}

	empty, err := svc.RegistrationsBySource(context.Background(), "2026-11-01", "")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Timezone != DefaultTimezone || empty.Since != "2026-11-01" || len(empty.Days) != 0 {
		t.Fatalf("future since: %+v", empty)
	}

	raw, err := json.Marshal(vn)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, secret := range []string{
		"boundary@test.com", "before@test.com", "inactive-marker", "deleted-marker",
		"tt.boundary", "tt.deleted", "fb-before", "newsletter-marker",
		onStart.ID.String(), deleted.ID.String(),
	} {
		if strings.Contains(body, secret) {
			t.Fatalf("response contains %q: %s", secret, body)
		}
	}
}

func TestRegistrationsBySource_RejectsBadQuery(t *testing.T) {
	_, svc := setupStatsDB(t)
	if _, err := svc.RegistrationsBySource(context.Background(), "", "Asia/Ho_Chi_Minh"); err == nil || !strings.Contains(err.Error(), ErrInvalidSince.Error()) {
		t.Fatalf("missing since: %v", err)
	}
	if _, err := svc.RegistrationsBySource(context.Background(), "2026-13-01", ""); err == nil {
		t.Fatal("expected invalid since")
	}
	if _, err := svc.RegistrationsBySource(context.Background(), "2026-10-01", "Not/AZone"); err == nil {
		t.Fatal("expected invalid timezone")
	}
	if _, err := svc.RegistrationsBySource(context.Background(), "2026-10-01", "Local"); err == nil {
		t.Fatal("expected Local to be rejected")
	}
	if _, err := NewService(nil).RegistrationsBySource(context.Background(), "2026-10-01", ""); err == nil {
		t.Fatal("expected unavailable")
	}
}
