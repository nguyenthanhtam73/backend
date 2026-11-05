package routine

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

func TestEveningTickBefore0400LandsOnPreviousDay(t *testing.T) {
	db := openRoutineAffiliateDB(t)
	repo := repository.NewRoutineEntryRepository(db)
	svc := NewService(nil, repo, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()
	uid := uuid.New()
	if err := db.Create(&domain.User{
		ID:       uid,
		Email:    uid.String() + "@example.com",
		Username: "eve" + uid.String()[:8],
		Provider: domain.AuthProviderLocal,
	}).Error; err != nil {
		t.Fatal(err)
	}

	vn := func(y int, m time.Month, d, hour, min int) time.Time {
		return time.Date(y, m, d, hour, min, 0, 0, streaktime.Location)
	}
	svc.now = func() time.Time { return vn(2026, 11, 4, 20, 0) }

	morning := []dto.RoutineStep{{ID: "wash", Title: "Rửa mặt", Category: "cleanser", Completed: true}}
	eveningOpen := []dto.RoutineStep{{ID: "cream", Title: "Dưỡng", Category: "moisturizer", Completed: false}}
	saved, err := svc.Upsert(ctx, uid, dto.PutRoutineRequest{
		RoutineDate: "2026-11-05",
		Morning:     morning,
		Evening:     eveningOpen,
		SaveKind:    "manual_edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.RoutineDate != "2026-11-04" {
		t.Fatalf("evening window saved %s", saved.RoutineDate)
	}

	svc.now = func() time.Time { return vn(2026, 11, 5, 1, 30) }
	eveningDone := []dto.RoutineStep{{ID: "cream", Title: "Dưỡng", Category: "moisturizer", Completed: true}}
	ticked, err := svc.Upsert(ctx, uid, dto.PutRoutineRequest{
		RoutineDate: "2026-11-05",
		Morning:     morning,
		Evening:     eveningDone,
		SaveKind:    "tick_only",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ticked.RoutineDate != "2026-11-04" || !ticked.Evening[0].Completed || !ticked.Saved {
		t.Fatalf("01:30 tick %+v", ticked)
	}

	var rows []domain.RoutineEntry
	if err := db.Where("user_id = ?", uid).Order("routine_date").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RoutineDate.UTC().Format("2006-01-02") != "2026-11-04" {
		t.Fatalf("stored dates %+v", rows)
	}

	current, err := svc.GetCurrent(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if current.RoutineDate != "2026-11-04" || !current.Saved || !current.Evening[0].Completed {
		t.Fatalf("current %+v", current)
	}
	history, err := svc.History(ctx, uid, 7)
	if err != nil {
		t.Fatal(err)
	}
	if history.To != "2026-11-04" || history.StreakDays != 1 {
		t.Fatalf("streak %+v", history)
	}

	svc.now = func() time.Time { return vn(2026, 11, 5, 3, 59) }
	if again, err := svc.Upsert(ctx, uid, dto.PutRoutineRequest{
		Morning:  morning,
		Evening:  eveningDone,
		SaveKind: "tick_only",
	}); err != nil || again.RoutineDate != "2026-11-04" {
		t.Fatalf("03:59 %+v %v", again, err)
	}

	svc.now = func() time.Time { return vn(2026, 11, 5, 4, 0) }
	rolled, err := svc.GetCurrent(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.RoutineDate != "2026-11-05" || rolled.Saved || rolled.Evening[0].Completed {
		t.Fatalf("04:00 read %+v", rolled)
	}
	created, err := svc.Upsert(ctx, uid, dto.PutRoutineRequest{
		RoutineDate: "2026-11-04",
		Morning:     morning,
		Evening:     eveningOpen,
		SaveKind:    "manual_edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.RoutineDate != "2026-11-05" {
		t.Fatalf("04:00 write %s", created.RoutineDate)
	}
	var stored []domain.RoutineEntry
	if err := db.Where("user_id = ?", uid).Order("routine_date").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("days after 04:00 %+v", stored)
	}
	prev := dto.RoutineFromDomain(&stored[0], true)
	next := dto.RoutineFromDomain(&stored[1], true)
	if prev.RoutineDate != "2026-11-04" || !prev.Evening[0].Completed {
		t.Fatalf("previous evening lost %+v", prev)
	}
	if next.RoutineDate != "2026-11-05" || next.Evening[0].Completed {
		t.Fatalf("new morning inherited the evening tick %+v", next)
	}
}
