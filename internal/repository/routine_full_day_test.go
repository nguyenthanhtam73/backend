package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestLatestFullRoutineDate(t *testing.T) {
	db := testRoutineDB(t)
	repo := NewRoutineEntryRepository(db)
	ctx := context.Background()
	uid := uuid.New()
	day := func(offset int) time.Time {
		return time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC).AddDate(0, 0, offset)
	}
	save := func(offset int, morning, evening string) {
		t.Helper()
		_, err := repo.UpsertForDay(ctx, &domain.RoutineEntry{
			UserID:      uid,
			RoutineDate: day(offset),
			Morning:     json.RawMessage(morning),
			Evening:     json.RawMessage(evening),
			Source:      "manual",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	full := `[{"id":"wash","title":"Rửa mặt","completed":true}]`
	partial := `[{"id":"wash","title":"Rửa mặt","completed":true},{"id":"spf","title":"Chống nắng","completed":false}]`

	got, err := repo.LatestFullRoutineDate(ctx, uid)
	if err != nil || got != nil {
		t.Fatalf("none: %v %v", got, err)
	}

	save(-5, full, full)
	save(-1, partial, `[]`)
	save(0, partial, full)
	got, err = repo.LatestFullRoutineDate(ctx, uid)
	if err != nil || got == nil || got.UTC().Format("2006-01-02") != day(-5).Format("2006-01-02") {
		t.Fatalf("partial newer hid the full day: %v %v", got, err)
	}

	save(-1, full, full)
	got, err = repo.LatestFullRoutineDate(ctx, uid)
	if err != nil || got == nil || got.UTC().Format("2006-01-02") != day(-1).Format("2006-01-02") {
		t.Fatalf("newer full day: %v %v", got, err)
	}

	if err := db.Where("user_id = ? AND routine_date = ?", uid, day(-1)).Delete(&domain.RoutineEntry{}).Error; err != nil {
		t.Fatal(err)
	}
	got, err = repo.LatestFullRoutineDate(ctx, uid)
	if err != nil || got == nil || got.UTC().Format("2006-01-02") != day(-5).Format("2006-01-02") {
		t.Fatalf("deleted full day still counted: %v %v", got, err)
	}

	other := uuid.New()
	if _, err := repo.UpsertForDay(ctx, &domain.RoutineEntry{
		UserID:      other,
		RoutineDate: day(0),
		Morning:     json.RawMessage(full),
		Evening:     json.RawMessage(full),
		Source:      "manual",
	}); err != nil {
		t.Fatal(err)
	}
	got, err = repo.LatestFullRoutineDate(ctx, uid)
	if err != nil || got == nil || got.UTC().Format("2006-01-02") != day(-5).Format("2006-01-02") {
		t.Fatalf("other user: %v %v", got, err)
	}
}
