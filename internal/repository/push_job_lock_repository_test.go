package repository

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPushJobLock_TryClaimAndRelease(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:push_job_lock?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.PushJobLock{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewPushJobLockRepository(db)
	ctx := context.Background()
	const day = "2026-07-17"

	ok, err := repo.TryClaim(ctx, domain.PushJobDailyReminder, day)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	ok, err = repo.TryClaim(ctx, domain.PushJobDailyReminder, day)
	if err != nil || ok {
		t.Fatalf("second claim same day should fail: ok=%v err=%v", ok, err)
	}

	if err := repo.ReleaseClaim(ctx, domain.PushJobDailyReminder, day); err != nil {
		t.Fatalf("release: %v", err)
	}
	ok, err = repo.TryClaim(ctx, domain.PushJobDailyReminder, day)
	if err != nil || !ok {
		t.Fatalf("reclaim after release: ok=%v err=%v", ok, err)
	}

	// Different job is independent.
	ok, err = repo.TryClaim(ctx, domain.PushJobStreakAtRisk, day)
	if err != nil || !ok {
		t.Fatalf("streak claim: ok=%v err=%v", ok, err)
	}
}

func TestPushJobLock_HourKeyFitsAndClaims(t *testing.T) {
	hourKey := time.Date(2026, 9, 7, 4, 0, 0, 0, time.UTC).Format(domain.PushJobHourKeyLayout)
	if len(hourKey) != 13 {
		t.Fatalf("hour key %q len=%d want 13", hourKey, len(hourKey))
	}
	if len(hourKey) > domain.PushJobRunKeyMaxLen {
		t.Fatalf("hour key %q len=%d exceeds last_run_date VARCHAR(%d)",
			hourKey, len(hourKey), domain.PushJobRunKeyMaxLen)
	}

	db, err := gorm.Open(sqlite.Open("file:push_job_hour?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.PushJobLock{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewPushJobLockRepository(db)
	ctx := context.Background()

	ok, err := repo.TryClaim(ctx, domain.PushJobD0D1Reminder, hourKey)
	if err != nil || !ok {
		t.Fatalf("hour claim: ok=%v err=%v", ok, err)
	}
	ok, err = repo.TryClaim(ctx, domain.PushJobD0D1Reminder, hourKey)
	if err != nil || ok {
		t.Fatalf("same hour should be held: ok=%v err=%v", ok, err)
	}

	nextHour := time.Date(2026, 9, 7, 5, 0, 0, 0, time.UTC).Format(domain.PushJobHourKeyLayout)
	ok, err = repo.TryClaim(ctx, domain.PushJobD0D1Reminder, nextHour)
	if err != nil || !ok {
		t.Fatalf("next hour must be claimable: ok=%v err=%v", ok, err)
	}
}

func TestPushJobLock_ExpiredLeaseCanBeStolen(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:push_job_lease?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.PushJobLock{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewPushJobLockRepository(db)
	ctx := context.Background()
	const day = "2026-07-17"

	ok, err := repo.TryClaim(ctx, domain.PushJobDailyReminder, day)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}

	// Simulate crashed pod: lease already expired.
	past := time.Now().UTC().Add(-time.Minute)
	if err := db.Model(&domain.PushJobLock{}).
		Where("job_name = ?", domain.PushJobDailyReminder).
		Updates(map[string]any{"expires_at": past}).Error; err != nil {
		t.Fatalf("expire: %v", err)
	}

	ok, err = repo.TryClaim(ctx, domain.PushJobDailyReminder, day)
	if err != nil || !ok {
		t.Fatalf("expired lease should be stealable: ok=%v err=%v", ok, err)
	}
}
