package premium

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type expiryStubUsers struct {
	user *domain.User
}

func (s expiryStubUsers) Create(context.Context, *domain.User) error { return nil }
func (s expiryStubUsers) GetByEmail(context.Context, string) (*domain.User, error) {
	return nil, nil
}
func (s expiryStubUsers) UsernameExists(context.Context, string) (bool, error) { return false, nil }
func (s expiryStubUsers) GetByID(context.Context, uuid.UUID) (*domain.User, error) {
	return s.user, nil
}

func TestPlanTier_RespectsExpiry(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(48 * time.Hour)
	uid := uuid.New()

	expired := &domain.User{
		ID:            uid,
		Email:         "e@test.com",
		Username:      "exp",
		PlanTier:      domain.PlanPremiumPlus,
		PlanExpiresAt: &past,
		IsActive:      true,
	}
	svc := NewService(expiryStubUsers{user: expired}, nil)
	tier, err := svc.PlanTier(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if tier != domain.PlanFree {
		t.Fatalf("expired → free, got %s", tier)
	}
	ok, reason, err := svc.CanUseFeature(context.Background(), uid, domain.FeatureAdvancedSkinAnalysis)
	if err != nil {
		t.Fatal(err)
	}
	if ok || reason != ReasonFeatureDenied {
		t.Fatalf("advanced skin should be denied after expiry, ok=%v reason=%q", ok, reason)
	}

	active := &domain.User{
		ID:            uid,
		Email:         "a@test.com",
		Username:      "act",
		PlanTier:      domain.PlanPremiumPlus,
		PlanExpiresAt: &future,
		IsActive:      true,
	}
	svc = NewService(expiryStubUsers{user: active}, nil)
	tier, err = svc.PlanTier(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if tier != domain.PlanPremiumPlus {
		t.Fatalf("active plus: %s", tier)
	}
	ok, _, err = svc.CanUseFeature(context.Background(), uid, domain.FeatureAdvancedSkinAnalysis)
	if err != nil || !ok {
		t.Fatalf("plus active should allow advanced skin: ok=%v err=%v", ok, err)
	}
}

func TestDowngradeExpiredPlans(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:plan_expiry_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
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
	svc := NewService(users, nil)
	svc.AttachPlanExpiryDeps(db, users, logs)

	past := time.Now().UTC().Add(-2 * time.Hour)
	future := time.Now().UTC().Add(48 * time.Hour)

	expired := &domain.User{
		Email:         "expired@test.com",
		Username:      "expired_u",
		PlanTier:      domain.PlanPremium,
		PlanExpiresAt: &past,
		IsActive:      true,
	}
	active := &domain.User{
		Email:         "active@test.com",
		Username:      "active_u",
		PlanTier:      domain.PlanPremiumPlus,
		PlanExpiresAt: &future,
		IsActive:      true,
	}
	lifetime := &domain.User{
		Email:    "life@test.com",
		Username: "life_u",
		PlanTier: domain.PlanPremium,
		IsActive: true,
	}
	for _, u := range []*domain.User{expired, active, lifetime} {
		if err := users.Create(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}

	n, err := svc.DowngradeExpiredPlans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("downgraded=%d want 1", n)
	}

	got, err := users.GetByID(context.Background(), expired.ID)
	if err != nil || got == nil {
		t.Fatalf("reload expired: %v", err)
	}
	if got.PlanTier != domain.PlanFree || got.PlanExpiresAt != nil {
		t.Fatalf("expired user not cleared: tier=%s expires=%v", got.PlanTier, got.PlanExpiresAt)
	}

	gotActive, _ := users.GetByID(context.Background(), active.ID)
	if gotActive.PlanTier != domain.PlanPremiumPlus {
		t.Fatalf("active should stay plus: %s", gotActive.PlanTier)
	}
	gotLife, _ := users.GetByID(context.Background(), lifetime.ID)
	if gotLife.PlanTier != domain.PlanPremium || gotLife.PlanExpiresAt != nil {
		t.Fatalf("lifetime grant should stay: %+v", gotLife)
	}
}
