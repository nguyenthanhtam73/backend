package adminmetrics

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

func setupMetricsDB(t *testing.T) (*gorm.DB, *repository.PaymentOrderRepository, *repository.GormUserRepository, *repository.PaymentOpsEventRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:admin_metrics_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.PaymentOrder{}, &domain.PaymentOpsEvent{}); err != nil {
		t.Fatal(err)
	}
	return db, repository.NewPaymentOrderRepository(db), repository.NewUserRepository(db), repository.NewPaymentOpsEventRepository(db)
}

func TestPaymentMetricsAggregates(t *testing.T) {
	_, orders, users, ops := setupMetricsDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	u := &domain.User{
		Email:    "prem@test.com",
		Username: "prem",
		PlanTier: domain.PlanPremium,
		IsActive: true,
	}
	exp := now.Add(48 * time.Hour)
	u.PlanExpiresAt = &exp
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	paid := &domain.PaymentOrder{
		UserID:          u.ID,
		InvoiceNumber:   "INV-PAID",
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		AmountVND:       99000,
		Currency:        "VND",
		Status:          domain.PaymentPaid,
		Provider:        domain.PaymentProviderSePay,
		CreatedAt:       now,
	}
	if err := orders.Create(ctx, paid); err != nil {
		t.Fatal(err)
	}
	failed := &domain.PaymentOrder{
		UserID:          u.ID,
		InvoiceNumber:   "INV-FAIL",
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		AmountVND:       99000,
		Currency:        "VND",
		Status:          domain.PaymentFailed,
		Provider:        domain.PaymentProviderSePay,
		CreatedAt:       now,
	}
	if err := orders.Create(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if err := ops.Create(ctx, &domain.PaymentOpsEvent{
		Kind:   domain.OpsKindWebhookError,
		Reason: "signature_invalid",
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(orders, users, ops)
	out, err := svc.PaymentMetrics(ctx, PaymentMetricsQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if out.RecentPaymentsTotal != 2 {
		t.Fatalf("recent_total=%d", out.RecentPaymentsTotal)
	}
	if len(out.RecentPayments) != 2 {
		t.Fatalf("recent_len=%d", len(out.RecentPayments))
	}
	if out.TodayPayments != 2 {
		t.Fatalf("today_payments=%d", out.TodayPayments)
	}
	if out.FailedCount != 1 {
		t.Fatalf("failed_count=%d", out.FailedCount)
	}
	if out.TotalRevenue != 99000 {
		t.Fatalf("total_revenue=%d", out.TotalRevenue)
	}
	if out.SuccessRate != 50 {
		t.Fatalf("success_rate=%v", out.SuccessRate)
	}
	if out.WebhookErrorsLast24h != 1 {
		t.Fatalf("webhook_errors=%d", out.WebhookErrorsLast24h)
	}
	if out.ActivePremiumCount != 1 {
		t.Fatalf("active_premium=%d", out.ActivePremiumCount)
	}
	if len(out.UpcomingExpiries) != 1 {
		t.Fatalf("upcoming=%d", len(out.UpcomingExpiries))
	}
}
