package subscription

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

func setupSubDB(t *testing.T) (*gorm.DB, *repository.GormUserRepository, *repository.SubscriptionRepository, *repository.PlanChangeLogRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:sub_lc_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.Subscription{}, &domain.PlanChangeLog{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	subs := repository.NewSubscriptionRepository(db)
	logs := repository.NewPlanChangeLogRepository(db)
	return db, users, subs, logs
}

func createFreeUser(t *testing.T, users *repository.GormUserRepository) *domain.User {
	t.Helper()
	u := &domain.User{
		Email:              uuid.New().String() + "@test.com",
		Username:           "u_" + uuid.New().String()[:8],
		PlanTier:           domain.PlanFree,
		SubscriptionStatus: domain.SubStatusNone,
		IsActive:           true,
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestStartTrial_Cancel_Renewal_CheckActive(t *testing.T) {
	db, users, subs, logs := setupSubDB(t)
	svc := NewService(db, users, subs, logs, 7, 3)
	ctx := context.Background()

	u := createFreeUser(t, users)

	// --- StartTrial ---
	plan, err := svc.StartTrial(ctx, u.ID, domain.PlanPremium)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Active || plan.Status != domain.SubStatusTrialing {
		t.Fatalf("trial plan: %+v", plan)
	}
	// DaysLeft counts until access ends (trial + grace).
	if plan.DaysLeft < 9 || plan.DaysLeft > 10 {
		t.Fatalf("trial days_left=%d want ~10 (7 trial + 3 grace)", plan.DaysLeft)
	}
	if plan.EligibleForTrial {
		t.Fatal("should not be eligible after starting trial")
	}
	if plan.TrialEndsAt == nil {
		t.Fatal("trial_ends_at required")
	}

	// Second trial rejected.
	if _, err := svc.StartTrial(ctx, u.ID, domain.PlanPremium); err != ErrNotEligible {
		t.Fatalf("second trial: %v", err)
	}

	// --- Cancel ---
	canceled, err := svc.CancelSubscription(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if canceled.Status != domain.SubStatusCanceled || !canceled.CancelAtPeriodEnd {
		t.Fatalf("canceled: %+v", canceled)
	}
	if !canceled.Active {
		t.Fatal("canceled user keeps access until grace ends")
	}
	if _, err := svc.CancelSubscription(ctx, u.ID); err != ErrAlreadyCanceled {
		t.Fatalf("double cancel: %v", err)
	}

	// --- Renew (SePay) clears cancel + extends ---
	beforeExpire := *canceled.PlanExpiresAt
	renewed, err := svc.HandleRenewal(ctx, RenewalInput{
		UserID:          u.ID,
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		ExternalRef:     "DD-TEST-INV-1",
		Provider:        domain.SubProviderSePay,
	})
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Status != domain.SubStatusActive || renewed.CanceledAt != nil {
		t.Fatalf("renewed: %+v", renewed)
	}
	if renewed.PlanExpiresAt == nil || !renewed.PlanExpiresAt.After(beforeExpire) {
		t.Fatalf("expiry not extended: before=%s after=%v", beforeExpire, renewed.PlanExpiresAt)
	}

	// Idempotent renew with same external_ref.
	again, err := svc.HandleRenewal(ctx, RenewalInput{
		UserID:          u.ID,
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		ExternalRef:     "DD-TEST-INV-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !again.PlanExpiresAt.Equal(*renewed.PlanExpiresAt) {
		t.Fatalf("idempotent renew changed expiry: %v vs %v", again.PlanExpiresAt, renewed.PlanExpiresAt)
	}

	snap, err := svc.CheckActivePlan(ctx, u.ID)
	if err != nil || snap == nil || !snap.Active {
		t.Fatalf("CheckActivePlan: %+v err=%v", snap, err)
	}
	if snap.GraceDaysConfigured != 3 || snap.TrialDaysConfigured != 7 {
		t.Fatalf("config echo: %+v", snap)
	}

	hist, err := subs.ListForUser(ctx, u.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) < 3 {
		t.Fatalf("history rows=%d want >= 3", len(hist))
	}
}

func TestDowngradePastGrace(t *testing.T) {
	db, users, subs, logs := setupSubDB(t)
	svc := NewService(db, users, subs, logs, 7, 3)
	ctx := context.Background()

	pastGrace := time.Now().UTC().Add(-4 * 24 * time.Hour)
	inGrace := time.Now().UTC().Add(-time.Hour)

	expired := &domain.User{
		Email:              "e1@test.com",
		Username:           "e1",
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      &pastGrace,
		SubscriptionStatus: domain.SubStatusPastDue,
		IsActive:           true,
	}
	graceUser := &domain.User{
		Email:              "e2@test.com",
		Username:           "e2",
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      &inGrace,
		SubscriptionStatus: domain.SubStatusPastDue,
		IsActive:           true,
	}
	for _, u := range []*domain.User{expired, graceUser} {
		if err := users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	n, err := svc.DowngradePastGrace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("downgraded=%d want 1", n)
	}

	got, _ := users.GetByID(ctx, expired.ID)
	if got.PlanTier != domain.PlanFree || got.SubscriptionStatus != domain.SubStatusExpired {
		t.Fatalf("expired not downgraded: %+v", got)
	}
	still, _ := users.GetByID(ctx, graceUser.ID)
	if still.PlanTier != domain.PlanPremium {
		t.Fatalf("grace user downgraded early: %+v", still)
	}

	// In-grace user still CheckActivePlan=true + InGrace.
	snap, err := svc.CheckActivePlan(ctx, graceUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Active || !snap.InGrace || snap.Status != domain.SubStatusPastDue {
		t.Fatalf("grace snapshot: %+v", snap)
	}
}
