package adminuser

import (
	"context"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupAdminUserTest(t *testing.T) (*Service, *domain.User, *domain.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:admin_user_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.PlanChangeLog{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	logs := repository.NewPlanChangeLogRepository(db)
	svc := NewService(db, users, logs, nil)

	actor := &domain.User{
		Email:    "admin@dadiary.test",
		Username: "admin_tester",
		PlanTier: domain.PlanFree,
		IsActive: true,
	}
	target := &domain.User{
		Email:    "user@dadiary.test",
		Username: "free_user",
		PlanTier: domain.PlanFree,
		IsActive: true,
	}
	if err := users.Create(context.Background(), actor); err != nil {
		t.Fatal(err)
	}
	if err := users.Create(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	return svc, actor, target
}

func TestUpdatePlan_GrantsPremiumAndLogs(t *testing.T) {
	svc, actor, target := setupAdminUserTest(t)

	res, err := svc.UpdatePlan(
		context.Background(),
		actor.ID,
		actor.Email,
		target.ID,
		dto.AdminUpdatePlanRequest{PlanTier: "premium", Reason: "qa grant"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.User.PlanTier != "premium" {
		t.Fatalf("plan=%s", res.User.PlanTier)
	}
	if res.Log.FromPlan != "free" || res.Log.ToPlan != "premium" {
		t.Fatalf("log=%+v", res.Log)
	}
	if res.Log.ActorEmail != actor.Email {
		t.Fatalf("actor email=%s", res.Log.ActorEmail)
	}

	detail, err := svc.Get(context.Background(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.RecentChanges) != 1 {
		t.Fatalf("logs=%d", len(detail.RecentChanges))
	}
}

func TestUpdatePlan_SamePlanConflict(t *testing.T) {
	svc, actor, target := setupAdminUserTest(t)
	_, err := svc.UpdatePlan(context.Background(), actor.ID, actor.Email, target.ID, dto.AdminUpdatePlanRequest{
		PlanTier: "free",
	})
	if err != ErrSamePlan {
		t.Fatalf("want ErrSamePlan, got %v", err)
	}
}

func TestSearch_FindsByEmail(t *testing.T) {
	svc, _, target := setupAdminUserTest(t)
	out, err := svc.Search(context.Background(), "user@dadiary", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if out.Total < 1 {
		t.Fatal("expected at least one match")
	}
	found := false
	for _, item := range out.Items {
		if item.ID == target.ID.String() {
			found = true
		}
	}
	if !found {
		t.Fatal("target not in search results")
	}
}
