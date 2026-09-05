package adminfunnel

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

func setupFunnelDB(t *testing.T) (*gorm.DB, *Service) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:admin_funnel_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.SkinCheck{}, &domain.PaymentOrder{}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(
		repository.NewUserRepository(db),
		repository.NewSkinCheckRepository(db),
		repository.NewPaymentOrderRepository(db),
	)
	return db, svc
}

func TestStats_LeakyBucketCounts(t *testing.T) {
	db, svc := setupFunnelDB(t)
	ctx := context.Background()

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, streaktime.Location)
	svc.now = func() time.Time { return now }
	users := repository.NewUserRepository(db)
	today := streaktime.DateOf(now)
	yesterday := today.AddDate(0, 0, -1)

	fresh := &domain.User{Email: "fresh@test.com", Username: "fresh", IsActive: true}
	if err := users.Create(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if err := users.SetCreatedAtForTest(ctx, fresh.ID, now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	d0User := &domain.User{Email: "d0@test.com", Username: "d0user", IsActive: true}
	if err := users.Create(ctx, d0User); err != nil {
		t.Fatal(err)
	}
	// Early on the previous VN day so this user is D0-eligible but outside the rolling 24h window.
	if err := users.SetCreatedAtForTest(ctx, d0User.ID, yesterday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	stale := &domain.User{Email: "stale@test.com", Username: "stale", IsActive: true}
	if err := users.Create(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if err := users.SetCreatedAtForTest(ctx, stale.ID, now.Add(-10*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	img, _ := json.Marshal([]string{"2026/09/05/check-in/d0user/a.jpg"})
	if err := db.Create(&domain.SkinCheck{
		UserID:    d0User.ID,
		ImageURLs: img,
		CheckDate: yesterday,
		CreatedAt: yesterday.Add(time.Hour), // same VN day, outside the rolling 24h window
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.SkinCheck{
		UserID:    stale.ID,
		ImageURLs: img,
		CheckDate: streaktime.DateOf(now.Add(-10 * 24 * time.Hour)),
		CreatedAt: now.Add(-10 * 24 * time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	paidAt := now.Add(-3 * 24 * time.Hour)
	if err := db.Create(&domain.PaymentOrder{
		UserID:          d0User.ID,
		InvoiceNumber:   "INV-PAID-7D",
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		AmountVND:       99000,
		Currency:        "VND",
		Status:          domain.PaymentPaid,
		Provider:        domain.PaymentProviderSePay,
		PaidAt:          &paidAt,
		CreatedAt:       paidAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	oldPaid := now.Add(-20 * 24 * time.Hour)
	if err := db.Create(&domain.PaymentOrder{
		UserID:          stale.ID,
		InvoiceNumber:   "INV-PAID-OLD",
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		AmountVND:       99000,
		Currency:        "VND",
		Status:          domain.PaymentPaid,
		Provider:        domain.PaymentProviderSePay,
		PaidAt:          &oldPaid,
		CreatedAt:       oldPaid,
	}).Error; err != nil {
		t.Fatal(err)
	}

	out, err := svc.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out.SignedUp1d != 1 {
		t.Fatalf("signed_up_1d=%d want 1", out.SignedUp1d)
	}
	if out.SignedUp7d != 2 {
		t.Fatalf("signed_up_7d=%d want 2", out.SignedUp7d)
	}
	if out.SkinCheckUsersEver != 2 {
		t.Fatalf("skin_check_users_ever=%d want 2", out.SkinCheckUsersEver)
	}
	if out.SkinCheckUsers1d != 0 {
		t.Fatalf("skin_check_users_1d=%d want 0", out.SkinCheckUsers1d)
	}
	if out.SkinCheckUsers7d != 1 {
		t.Fatalf("skin_check_users_7d=%d want 1 (d0User check yesterday)", out.SkinCheckUsers7d)
	}
	if out.D0CheckinUsers != 2 {
		t.Fatalf("d0_checkin_users=%d want 2", out.D0CheckinUsers)
	}
	if out.D0CheckinUsers7d != 1 {
		t.Fatalf("d0_checkin_users_7d=%d want 1", out.D0CheckinUsers7d)
	}
	if out.D1EligibleUsers != 2 { // d0User + stale; fresh signed today
		t.Fatalf("d1_eligible_users=%d want 2", out.D1EligibleUsers)
	}
	if out.D1CheckinUsers != 0 {
		t.Fatalf("d1_checkin_users=%d want 0", out.D1CheckinUsers)
	}
	if out.PaidOrders7d != 1 {
		t.Fatalf("paid_orders_7d=%d want 1", out.PaidOrders7d)
	}
	if out.PaywallViews != nil {
		t.Fatalf("paywall_views should be null")
	}
	if out.Notes.Paywall == "" || out.Notes.Calendar != "Asia/Ho_Chi_Minh" {
		t.Fatalf("notes missing: %#v", out.Notes)
	}
}

func TestStats_Unavailable(t *testing.T) {
	var svc *Service
	if _, err := svc.Stats(context.Background()); err != ErrUnavailable {
		t.Fatalf("nil service err=%v", err)
	}
	svc = NewService(nil, nil, nil)
	if _, err := svc.Stats(context.Background()); err != ErrUnavailable {
		t.Fatalf("nil deps err=%v", err)
	}
}
