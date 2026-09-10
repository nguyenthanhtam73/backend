package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testRoutineDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:routine_upsert_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.RoutineEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_routine_entries_user_date_live
		ON routine_entries (user_id, routine_date)
		WHERE deleted_at IS NULL
	`).Error; err != nil {
		t.Fatalf("unique index: %v", err)
	}
	return db
}

func TestUpsertForDay_ConcurrentWritesOneRow(t *testing.T) {
	db := testRoutineDB(t)
	repo := NewRoutineEntryRepository(db)
	ctx := context.Background()
	uid := uuid.New()
	day := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		notes := fmt.Sprintf("n-%d", i)
		go func(n string) {
			defer wg.Done()
			_, err := repo.UpsertForDay(ctx, &domain.RoutineEntry{
				UserID:      uid,
				RoutineDate: day,
				Morning:     json.RawMessage(`[{"id":"1","title":"cleanser"}]`),
				Notes:       n,
				Source:      "manual",
			})
			if err != nil {
				errCh <- err
			}
		}(notes)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("upsert: %v", err)
	}

	var n int64
	if err := db.Model(&domain.RoutineEntry{}).Where("user_id = ?", uid).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 live row, got %d", n)
	}
	got, err := repo.GetByUserAndDate(ctx, uid, day)
	if err != nil || got == nil {
		t.Fatalf("get: %v row=%v", err, got)
	}
}

func TestIsUniqueConflict(t *testing.T) {
	if IsUniqueConflict(nil) {
		t.Fatal("nil")
	}
	if !IsUniqueConflict(gorm.ErrDuplicatedKey) {
		t.Fatal("gorm duplicated")
	}
}
