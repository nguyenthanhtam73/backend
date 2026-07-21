package payment

import (
	"context"
	"encoding/json"
	"sync"
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

func setupPaymentFulfill(t *testing.T) (*Service, *domain.User, *repository.GormUserRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:pay_fulfill_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.PaymentOrder{}, &domain.PlanChangeLog{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	orders := repository.NewPaymentOrderRepository(db)
	logs := repository.NewPlanChangeLogRepository(db)
	cfg := &config.Config{
		SePay: config.SePayConfig{
			MerchantID: "SP-TEST",
			SecretKey:  "spsk_test",
			Env:        "sandbox",
		},
	}
	svc := NewService(db, cfg, orders, users, logs)

	u := &domain.User{
		Email:    "payer@test.com",
		Username: "payer",
		PlanTier: domain.PlanFree,
		IsActive: true,
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return svc, u, users
}

func seedOrder(
	t *testing.T,
	svc *Service,
	userID uuid.UUID,
	invoice string,
	tier domain.PlanTier,
	interval domain.BillingInterval,
	amount int64,
) {
	t.Helper()
	order := &domain.PaymentOrder{
		UserID:          userID,
		InvoiceNumber:   invoice,
		PlanTier:        tier,
		BillingInterval: interval,
		AmountVND:       amount,
		Currency:        "VND",
		Status:          domain.PaymentPending,
		Provider:        domain.PaymentProviderSePay,
	}
	if err := svc.orders.Create(context.Background(), order); err != nil {
		t.Fatal(err)
	}
}

func mustIPN(invoice, amount string) []byte {
	payload := IPNPayload{NotificationType: "ORDER_PAID"}
	payload.Order.OrderInvoiceNumber = invoice
	payload.Order.OrderStatus = "CAPTURED"
	payload.Order.OrderAmount = amount
	payload.Order.ID = "ord-" + invoice
	payload.Transaction.TransactionID = "tx-" + invoice
	b, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return b
}

func TestFulfillPaidOrder_SetsExpiryMonthly(t *testing.T) {
	svc, user, users := setupPaymentFulfill(t)
	invoice := "DD-EXP-MONTH"
	seedOrder(t, svc, user.ID, invoice, domain.PlanPremium, domain.BillingMonthly, 99000)

	before := time.Now().UTC()
	if err := svc.HandleSePayWebhook(context.Background(), "spsk_test", mustIPN(invoice, "99000")); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC()

	got, err := users.GetByID(context.Background(), user.ID)
	if err != nil || got == nil {
		t.Fatalf("user: %v", err)
	}
	if got.PlanTier != domain.PlanPremium {
		t.Fatalf("tier=%s", got.PlanTier)
	}
	if got.PlanExpiresAt == nil {
		t.Fatal("expected plan_expires_at")
	}
	minExp := before.Add(30 * 24 * time.Hour).Add(-2 * time.Second)
	maxExp := after.Add(30 * 24 * time.Hour).Add(2 * time.Second)
	if got.PlanExpiresAt.Before(minExp) || got.PlanExpiresAt.After(maxExp) {
		t.Fatalf("expires=%s not in [%s,%s]", got.PlanExpiresAt, minExp, maxExp)
	}
}

func TestFulfillPaidOrder_IdempotentIPNDoesNotDoubleExtend(t *testing.T) {
	svc, user, users := setupPaymentFulfill(t)
	invoice := "DD-IDEM-1"
	seedOrder(t, svc, user.ID, invoice, domain.PlanPremium, domain.BillingMonthly, 99000)

	raw := mustIPN(invoice, "99000")
	if err := svc.HandleSePayWebhook(context.Background(), "spsk_test", raw); err != nil {
		t.Fatal(err)
	}
	first, _ := users.GetByID(context.Background(), user.ID)
	exp1 := *first.PlanExpiresAt

	if err := svc.HandleSePayWebhook(context.Background(), "spsk_test", raw); err != nil {
		t.Fatal(err)
	}
	second, _ := users.GetByID(context.Background(), user.ID)
	if !second.PlanExpiresAt.Equal(exp1) {
		t.Fatalf("replay extended expiry: %s → %s", exp1, *second.PlanExpiresAt)
	}
}

func TestFulfillPaidOrder_ConcurrentIPNsNoDowngrade(t *testing.T) {
	svc, user, users := setupPaymentFulfill(t)

	// Premium+ and Premium settle concurrently — user must remain Premium+.
	seedOrder(t, svc, user.ID, "DD-RACE-PLUS", domain.PlanPremiumPlus, domain.BillingMonthly, 159000)
	seedOrder(t, svc, user.ID, "DD-RACE-PREM", domain.PlanPremium, domain.BillingMonthly, 99000)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, inv := range []struct {
		invoice string
		amount  string
	}{
		{"DD-RACE-PLUS", "159000"},
		{"DD-RACE-PREM", "99000"},
	} {
		wg.Add(1)
		go func(invoice, amount string) {
			defer wg.Done()
			errs <- svc.HandleSePayWebhook(context.Background(), "spsk_test", mustIPN(invoice, amount))
		}(inv.invoice, inv.amount)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	got, err := users.GetByID(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanTier != domain.PlanPremiumPlus {
		t.Fatalf("race left tier=%s want premium_plus", got.PlanTier)
	}
}
