package payment

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestPaymentSuccessAlertOmitsEmail(t *testing.T) {
	const payerEmail = "payer@gmail.com"
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	orderID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	const invoice = "DD-PAY-CODE-1"

	order := &domain.PaymentOrder{
		ID:              orderID,
		UserID:          userID,
		InvoiceNumber:   invoice,
		AmountVND:       99000,
		BillingInterval: domain.BillingMonthly,
	}
	ev := paymentSuccessAlert(order, domain.PlanPremium)

	wantMsg := "User 11111111-1111-1111-1111-111111111111 nâng cấp premium thành công, amount 99000 VND (monthly), invoice DD-PAY-CODE-1"
	if ev.Message != wantMsg {
		t.Fatalf("message=%q", ev.Message)
	}
	if ev.Fields["user_id"] != userID.String() {
		t.Fatalf("user_id=%v", ev.Fields["user_id"])
	}
	if ev.Fields["invoice"] != invoice {
		t.Fatalf("invoice=%v", ev.Fields["invoice"])
	}
	if _, ok := ev.Fields["user"]; ok {
		t.Fatal("user field must not carry the payer email")
	}
	assertNoEmail(t, payerEmail, ev.Message, ev.Detail)
	for k, v := range ev.Fields {
		s, _ := v.(string)
		assertNoEmail(t, payerEmail, k, s)
	}

	rec := &recordingAlerter{}
	svc := &Service{alerter: rec}
	svc.notifyPaymentSuccess(context.Background(), order, domain.PlanPremium)

	deadline := time.Now().Add(2 * time.Second)
	var got int
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		got = len(rec.events)
		rec.mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.events) != 1 {
		t.Fatalf("want 1 alert, got %d", len(rec.events))
	}
	sent := rec.events[0]
	if sent.Message != wantMsg {
		t.Fatalf("sent message=%q", sent.Message)
	}
	if sent.Fields["user_id"] != userID.String() || sent.Fields["invoice"] != invoice {
		t.Fatalf("sent fields=%v", sent.Fields)
	}
	assertNoEmail(t, payerEmail, sent.Message)
}

func assertNoEmail(t *testing.T, email string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if strings.Contains(p, email) || strings.Contains(p, "@") {
			t.Fatalf("email leaked in %q", p)
		}
	}
}
