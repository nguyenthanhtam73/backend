package payment

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupExpirePending(t *testing.T) (*Service, *repository.PaymentOrderRepository, *domain.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:pay_expire_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.PaymentOrder{}, &domain.PlanChangeLog{}, &domain.Subscription{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	orders := repository.NewPaymentOrderRepository(db)
	logs := repository.NewPlanChangeLogRepository(db)
	cfg := &config.Config{
		SePay: config.SePayConfig{
			MerchantID:      "SP-TEST",
			SecretKey:       "spsk_test",
			Env:             "sandbox",
			CheckoutEnabled: true,
		},
		PendingOrderExpiry: config.PendingOrderExpiryConfig{Enabled: true, TTLHours: 72},
	}
	svc := NewService(db, cfg, orders, users, logs)
	u := &domain.User{
		Email:    "expire@test.com",
		Username: "expire",
		IsActive: true,
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return svc, orders, u
}

func insertOrder(
	t *testing.T,
	orders *repository.PaymentOrderRepository,
	userID uuid.UUID,
	invoice string,
	status domain.PaymentOrderStatus,
	createdAt time.Time,
) {
	t.Helper()
	row := &domain.PaymentOrder{
		UserID:          userID,
		InvoiceNumber:   invoice,
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		AmountVND:       99000,
		Currency:        "VND",
		Status:          status,
		Provider:        domain.PaymentProviderSePay,
	}
	if err := orders.Create(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	if !createdAt.IsZero() {
		if err := orders.SetCreatedAtForTest(context.Background(), invoice, createdAt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExpireStalePending_ExpiresOldPendingOnly(t *testing.T) {
	svc, orders, user := setupExpirePending(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	old := now.Add(-80 * time.Hour)
	fresh := now.Add(-2 * time.Hour)
	paidOld := now.Add(-100 * time.Hour)

	insertOrder(t, orders, user.ID, "DD-OLD-PEND", domain.PaymentPending, old)
	insertOrder(t, orders, user.ID, "DD-FRESH-PEND", domain.PaymentPending, fresh)
	insertOrder(t, orders, user.ID, "DD-OLD-PAID", domain.PaymentPaid, paidOld)
	insertOrder(t, orders, user.ID, "DD-OLD-CANCEL", domain.PaymentCancelled, paidOld)

	res, err := svc.ExpireStalePending(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Expired != 1 {
		t.Fatalf("expired=%d want 1", res.Expired)
	}
	if res.PendingFresh != 1 {
		t.Fatalf("fresh=%d want 1", res.PendingFresh)
	}

	oldRow, _ := orders.GetByInvoiceNumber(context.Background(), "DD-OLD-PEND")
	if oldRow == nil || oldRow.Status != domain.PaymentExpired {
		t.Fatalf("old pending status=%v", oldRow)
	}
	freshRow, _ := orders.GetByInvoiceNumber(context.Background(), "DD-FRESH-PEND")
	if freshRow == nil || freshRow.Status != domain.PaymentPending {
		t.Fatalf("fresh pending touched: %v", freshRow)
	}
	paidRow, _ := orders.GetByInvoiceNumber(context.Background(), "DD-OLD-PAID")
	if paidRow == nil || paidRow.Status != domain.PaymentPaid {
		t.Fatalf("paid touched: %v", paidRow)
	}
	cancelRow, _ := orders.GetByInvoiceNumber(context.Background(), "DD-OLD-CANCEL")
	if cancelRow == nil || cancelRow.Status != domain.PaymentCancelled {
		t.Fatalf("cancelled touched: %v", cancelRow)
	}

	again, err := svc.ExpireStalePending(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if again.Expired != 0 {
		t.Fatalf("second run expired=%d want 0", again.Expired)
	}
}

func TestExpireStalePending_LatePaidIPNStillFulfills(t *testing.T) {
	svc, orders, user := setupExpirePending(t)
	now := time.Now().UTC()
	insertOrder(t, orders, user.ID, "DD-LATE-PAY", domain.PaymentPending, now.Add(-80*time.Hour))

	if _, err := svc.ExpireStalePending(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	got, _ := orders.GetByInvoiceNumber(context.Background(), "DD-LATE-PAY")
	if got.Status != domain.PaymentExpired {
		t.Fatalf("status=%s", got.Status)
	}

	if err := svc.HandleSePayWebhook(context.Background(), "spsk_test", mustIPN("DD-LATE-PAY", "99000")); err != nil {
		t.Fatal(err)
	}
	got, _ = orders.GetByInvoiceNumber(context.Background(), "DD-LATE-PAY")
	if got.Status != domain.PaymentPaid {
		t.Fatalf("late IPN did not pay expired order: %s", got.Status)
	}
}

func TestPendingOrderTTL_Clamp(t *testing.T) {
	if PendingOrderTTL(nil) != DefaultPendingOrderTTL {
		t.Fatalf("nil cfg ttl=%s", PendingOrderTTL(nil))
	}
	if PendingOrderTTL(&config.Config{PendingOrderExpiry: config.PendingOrderExpiryConfig{TTLHours: 3}}) != 24*time.Hour {
		t.Fatal("min clamp")
	}
	if PendingOrderTTL(&config.Config{PendingOrderExpiry: config.PendingOrderExpiryConfig{TTLHours: 400}}) != 168*time.Hour {
		t.Fatal("max clamp")
	}
}
