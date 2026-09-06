package checkinreminder

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/email"
	"github.com/dadiary/backend/internal/streaktime"
)

type stubMailer struct {
	mu      sync.Mutex
	sent    []email.Message
	ready   bool
	sendErr error
}

func (m *stubMailer) Configured() bool { return m != nil && m.ready }

func (m *stubMailer) Send(_ context.Context, msg email.Message) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func TestDeliverDue_NoESPIsNoOp(t *testing.T) {
	now := streaktime.Now()
	svc, users, _, _ := setupReminderSvc(t, now)
	createUser(t, users, "esp@test.com", StartOfVNDay(now).Add(20*time.Minute))
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.EmailSent != 0 || res.EmailFailed != 0 {
		t.Fatalf("no-op email: %+v", res)
	}
	if res.Candidates < 1 {
		t.Fatalf("expected due candidates: %+v", res)
	}
}

func TestDeliverDue_SendsOnceAndSkipsCheckedIn(t *testing.T) {
	now := streaktime.Now()
	svc, users, checks, db := setupReminderSvc(t, now)
	u := createUser(t, users, "once@test.com", StartOfVNDay(now).Add(15*time.Minute))
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}

	mailer := &stubMailer{ready: true}
	receipts := repository.NewEmailSendReceiptRepository(db)
	svc.AttachOutbound(mailer, receipts, nil, email.NewUnsubscribeSigner("secret", "https://api.test"), "https://dadiary.vn/check-in")

	first, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.EmailSent != 1 {
		t.Fatalf("first send: %+v", first)
	}
	if len(mailer.sent) != 1 || !strings.Contains(mailer.sent[0].Text, "https://dadiary.vn/check-in") {
		t.Fatalf("cta missing: %+v", mailer.sent)
	}
	if mailer.sent[0].Unsubscribe == "" {
		t.Fatal("expected unsubscribe link")
	}

	second, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.EmailSent != 0 || second.EmailSkipped < 1 {
		t.Fatalf("idempotent: %+v", second)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("double send: %d", len(mailer.sent))
	}

	check := &domain.SkinCheck{
		UserID:    u.ID,
		ImageURLs: json.RawMessage(`[]`),
		CheckDate: streaktime.DateOf(now),
	}
	if err := checks.CreateWithAnalysis(context.Background(), check, &domain.SkinAnalysis{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.EmailSent != 0 {
		t.Fatalf("checked in should skip: %+v", after)
	}
}

func TestDeliverDue_SkipUnsubscribedAndReleaseOnFail(t *testing.T) {
	now := streaktime.Now()
	svc, users, _, db := setupReminderSvc(t, now)
	u := createUser(t, users, "unsub@test.com", StartOfVNDay(now).Add(10*time.Minute))
	at := time.Now().UTC()
	if err := users.SetEmailUnsubscribedAt(context.Background(), u.ID, at); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	mailer := &stubMailer{ready: true}
	receipts := repository.NewEmailSendReceiptRepository(db)
	svc.AttachOutbound(mailer, receipts, nil, nil, "https://dadiary.vn/check-in")
	res, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.EmailSent != 0 || len(mailer.sent) != 0 {
		t.Fatalf("unsub should skip: %+v sent=%d", res, len(mailer.sent))
	}

	u2 := createUser(t, users, "fail@test.com", StartOfVNDay(now).Add(12*time.Minute))
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	mailer.sendErr = errors.New("boom")
	failRes, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if failRes.EmailFailed < 1 {
		t.Fatalf("expected fail: %+v", failRes)
	}
	ok, err := receipts.HasSent(context.Background(), u2.ID, "d0")
	if err != nil || ok {
		t.Fatalf("failed send must release claim: sent=%v err=%v", ok, err)
	}
}

func TestChannels_EmailOnAndJobOff(t *testing.T) {
	now := streaktime.Now()
	svc, users, _, _ := setupReminderSvc(t, now)
	svc.SetEmailConfigured(true)
	svc.SetJobEnabled(false)
	u := createUser(t, users, "ch@test.com", StartOfVNDay(now).Add(5*time.Minute))
	res, err := svc.GetForUser(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Channels.Email || res.Channels.EmailReason != "" {
		t.Fatalf("email on: %+v", res.Channels)
	}
	if res.Channels.PushD0D1Specific {
		t.Fatalf("job off should hide specific push: %+v", res.Channels)
	}
}
