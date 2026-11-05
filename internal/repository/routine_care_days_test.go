package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

func TestFullRoutineDaysCountsOneFinishedSession(t *testing.T) {
	db := testRoutineDB(t)
	repo := NewRoutineEntryRepository(db)
	ctx := context.Background()
	today := streaktime.Today()
	full := `[{"id":"wash","title":"Rửa mặt","completed":true}]`
	open := `[{"id":"wash","title":"Rửa mặt","completed":true},{"id":"spf","title":"Chống nắng","completed":false}]`
	legacy := `["Rửa mặt","Kem"]`

	a := uuid.New()
	b := uuid.New()
	c := uuid.New()
	put := func(user uuid.UUID, day time.Time, morning, evening string) {
		t.Helper()
		row := &domain.RoutineEntry{
			UserID:      user,
			RoutineDate: streaktime.DateOf(day),
			Morning:     json.RawMessage(morning),
			Evening:     json.RawMessage(evening),
			Source:      "manual",
		}
		if err := db.Omit("User").Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	put(a, today, full, full)
	put(a, today.AddDate(0, 0, -1), full, `[]`)
	put(a, today.AddDate(0, 0, -2), open, full)
	put(b, today, open, `[]`)
	put(b, today.AddDate(0, 0, -1), full, open)
	put(b, today.AddDate(0, 0, -2), full, full)
	put(c, today, legacy, `[]`)

	got, err := repo.FullRoutineDays(ctx, []uuid.UUID{a, b, c, a}, today.AddDate(0, 0, -10))
	if err != nil {
		t.Fatal(err)
	}
	wantA := today.Format("2006-01-02") + "," + today.AddDate(0, 0, -1).Format("2006-01-02") + "," + today.AddDate(0, 0, -2).Format("2006-01-02")
	if days := dateKeys(got[a]); days != wantA {
		t.Fatalf("A %s", days)
	}
	wantB := today.AddDate(0, 0, -1).Format("2006-01-02") + "," + today.AddDate(0, 0, -2).Format("2006-01-02")
	if days := dateKeys(got[b]); days != wantB {
		t.Fatalf("B partial today counted: %s", days)
	}
	if len(got[c]) != 0 {
		t.Fatalf("legacy %v", got[c])
	}
}

func dateKeys(days []time.Time) string {
	out := ""
	for i, day := range days {
		if i > 0 {
			out += ","
		}
		out += day.UTC().Format("2006-01-02")
	}
	return out
}
