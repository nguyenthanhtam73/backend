package premium

import (
	"context"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type stubUserRepo struct {
	tier domain.PlanTier
}

func (s stubUserRepo) Create(context.Context, *domain.User) error { return nil }
func (s stubUserRepo) GetByEmail(context.Context, string) (*domain.User, error) {
	return nil, nil
}
func (s stubUserRepo) UsernameExists(context.Context, string) (bool, error) { return false, nil }
func (s stubUserRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	return &domain.User{ID: id, PlanTier: s.tier, Email: "t@example.com", Username: "t"}, nil
}

func TestGetRemainingQuota_FreeFromUserUsage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:premium_quota?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.UserUsage{}); err != nil {
		t.Fatal(err)
	}
	usages := repository.NewUserUsageRepository(db)
	uid := uuid.New()
	svc := NewService(stubUserRepo{tier: domain.PlanFree}, usages)

	q, err := svc.GetRemainingQuota(context.Background(), uid, domain.FeatureAIRoutineSuggestion)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Allowed || q.Remaining != 3 || q.Limit != 3 {
		t.Fatalf("fresh free suggest: %+v", q)
	}

	for i := 0; i < 3; i++ {
		if err := svc.IncrementUsage(context.Background(), uid, domain.FeatureAIRoutineSuggestion); err != nil {
			t.Fatalf("inc %d: %v", i, err)
		}
	}
	ok, reason, err := svc.CanUseFeature(context.Background(), uid, domain.FeatureAIRoutineSuggestion)
	if err != nil {
		t.Fatal(err)
	}
	if ok || reason != ReasonQuotaExceeded {
		t.Fatalf("want denied quota_exceeded, got ok=%v reason=%q", ok, reason)
	}
	if err := svc.AssertFeature(context.Background(), uid, domain.FeatureAIRoutineSuggestion); err != ErrQuotaExceeded {
		t.Fatalf("assert: %v", err)
	}
}

func TestGetRemainingQuota_PremiumUnlimited(t *testing.T) {
	svc := NewService(stubUserRepo{tier: domain.PlanPremium}, nil)
	uid := uuid.New()
	q, err := svc.GetRemainingQuota(context.Background(), uid, domain.FeatureEditRoutine)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Allowed || !q.Unlimited {
		t.Fatalf("premium edit: %+v", q)
	}
	// Increment must no-op without usages repo when unlimited.
	if err := svc.IncrementUsage(context.Background(), uid, domain.FeatureEditRoutine); err != nil {
		t.Fatalf("unlimited increment: %v", err)
	}
}
