package repository

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testUserUsageDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.UserUsage{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestUserUsage_IncrementAndGetRemaining(t *testing.T) {
	db := testUserUsageDB(t)
	repo := NewUserUsageRepository(db)
	ctx := context.Background()
	uid := uuid.New()
	feature := string(domain.FeatureAIRoutineSuggestion)
	limit := 3

	view, err := repo.GetUsage(ctx, uid, feature, limit)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.UsageCount != 0 || view.Remaining != 3 {
		t.Fatalf("empty view: %+v", view)
	}

	for i := 1; i <= 3; i++ {
		n, ok, err := repo.IncrementUsage(ctx, uid, feature, limit)
		if err != nil || !ok {
			t.Fatalf("inc %d: n=%d ok=%v err=%v", i, n, ok, err)
		}
		if n != i {
			t.Fatalf("count want %d got %d", i, n)
		}
	}

	n, ok, err := repo.IncrementUsage(ctx, uid, feature, limit)
	if err != nil {
		t.Fatalf("over: %v", err)
	}
	if ok || n != 3 {
		t.Fatalf("expected block at 3, got ok=%v n=%d", ok, n)
	}

	view, err = repo.GetUsage(ctx, uid, feature, limit)
	if err != nil {
		t.Fatalf("get2: %v", err)
	}
	if view.UsageCount != 3 || view.Remaining != 0 {
		t.Fatalf("exhausted: %+v", view)
	}
}

func TestUserUsage_ResetMonthlyUsage(t *testing.T) {
	db := testUserUsageDB(t)
	repo := NewUserUsageRepository(db)
	ctx := context.Background()
	uid := uuid.New()

	pastStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	pastEnd := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Create(&domain.UserUsage{
		ID:          uuid.New(),
		UserID:      uid,
		FeatureKey:  string(domain.FeatureEditRoutine),
		UsageCount:  5,
		PeriodKey:   "2026-05",
		PeriodStart: pastStart,
		PeriodEnd:   pastEnd,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	currentStart, _, _ := domain.CurrentUTCMonthPeriod(time.Now())
	deleted, err := repo.ResetMonthlyUsage(ctx, currentStart)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if deleted < 1 {
		t.Fatalf("expected deleted >= 1, got %d", deleted)
	}
}

func TestCurrentUTCMonthPeriod(t *testing.T) {
	start, end, key := domain.CurrentUTCMonthPeriod(time.Date(2026, 7, 19, 15, 0, 0, 0, time.UTC))
	if start.Format("2006-01-02") != "2026-07-01" {
		t.Fatalf("start %s", start)
	}
	if end.Format("2006-01-02") != "2026-08-01" {
		t.Fatalf("end %s", end)
	}
	if key != "2026-07" {
		t.Fatalf("key %s", key)
	}
}
