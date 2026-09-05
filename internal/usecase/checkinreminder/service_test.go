package checkinreminder

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

func setupReminderSvc(t *testing.T, now time.Time) (*Service, *repository.GormUserRepository, *repository.GormSkinCheckRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:remind_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.SkinCheck{},
		&domain.CheckInReminderFlag{},
	); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	checks := repository.NewSkinCheckRepository(db)
	flags := repository.NewCheckInReminderRepository(db)
	svc := NewService(users, checks, flags, false)
	svc.now = func() time.Time { return now }
	return svc, users, checks
}

func createUser(t *testing.T, users *repository.GormUserRepository, email string, createdAt time.Time) *domain.User {
	t.Helper()
	u := &domain.User{
		Email:    email,
		Username: email,
		IsActive: true,
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if err := users.SetCreatedAtForTest(context.Background(), u.ID, createdAt); err != nil {
		t.Fatal(err)
	}
	got, err := users.GetByID(context.Background(), u.ID)
	if err != nil || got == nil {
		t.Fatalf("reload user: %v", err)
	}
	return got
}

func TestGetForUser_MarksD0AndClearsAfterCheckIn(t *testing.T) {
	now := streaktime.Now()
	svc, users, checks := setupReminderSvc(t, now)
	signup := StartOfVNDay(now).Add(30 * time.Minute)
	if !now.After(signup) {
		signup = StartOfVNDay(now)
	}
	u := createUser(t, users, "d0@test.com", signup)

	res, err := svc.GetForUser(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "d0" || !res.Due || res.CheckedInToday {
		t.Fatalf("D0 due: %+v", res)
	}
	if res.Channels.Email || res.Channels.PushD0D1Specific {
		t.Fatalf("channels should document missing email/specific push: %+v", res.Channels)
	}
	if res.Channels.EmailReason != "no_outbound_email" {
		t.Fatalf("email_reason=%s", res.Channels.EmailReason)
	}

	check := &domain.SkinCheck{
		UserID:    u.ID,
		ImageURLs: json.RawMessage(`[]`),
		CheckDate: streaktime.DateOf(now),
	}
	if err := checks.CreateWithAnalysis(context.Background(), check, &domain.SkinAnalysis{}); err != nil {
		t.Fatal(err)
	}

	after, err := svc.GetForUser(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Kind != "d0" || after.Due || !after.CheckedInToday {
		t.Fatalf("after check-in: %+v", after)
	}
}

func TestRefreshWindow_SelectsD0AndD1AndClearsPast(t *testing.T) {
	now := streaktime.Now()
	svc, users, _ := setupReminderSvc(t, now)
	todayStart := StartOfVNDay(now)

	d0 := createUser(t, users, "new@test.com", todayStart.Add(20*time.Minute))
	d1 := createUser(t, users, "yest@test.com", todayStart.Add(-2*time.Hour))
	old := createUser(t, users, "old@test.com", todayStart.Add(-80*time.Hour))

	// Seed a stale due flag on the old user so refresh must clear it.
	if err := svc.flags.Upsert(context.Background(), &domain.CheckInReminderFlag{
		UserID:     old.ID,
		Kind:       "d1",
		Due:        true,
		SignupDate: streaktime.DateOf(old.CreatedAt),
		ComputedOn: streaktime.DateOf(now.Add(-24 * time.Hour)),
		ComputedAt: now.Add(-24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.RefreshWindow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.DueD0 != 1 || res.DueD1 != 1 {
		t.Fatalf("refresh counts: %+v (d0=%s d1=%s old=%s)", res, d0.ID, d1.ID, old.ID)
	}
	if res.Cleared < 1 {
		t.Fatalf("expected old flag cleared: %+v", res)
	}

	oldFlag, err := svc.flags.GetByUserID(context.Background(), old.ID)
	if err != nil || oldFlag == nil {
		t.Fatalf("old flag: %v", err)
	}
	if oldFlag.Due || oldFlag.Kind != "none" {
		t.Fatalf("old flag not cleared: %+v", oldFlag)
	}

	again, err := svc.RefreshWindow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again.DueD0 != 1 || again.DueD1 != 1 {
		t.Fatalf("idempotent refresh: %+v", again)
	}
}
