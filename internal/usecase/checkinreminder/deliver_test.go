package checkinreminder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
	mu       sync.Mutex
	sent     []email.Message
	attempts int
	ready    bool
	sendErr  error
}

func (m *stubMailer) Configured() bool { return m != nil && m.ready }

func (m *stubMailer) Send(_ context.Context, msg email.Message) error {
	m.mu.Lock()
	m.attempts++
	if m.sendErr != nil {
		err := m.sendErr
		m.mu.Unlock()
		return err
	}
	m.sent = append(m.sent, msg)
	m.mu.Unlock()
	return nil
}

func (m *stubMailer) Attempts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.attempts
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

func TestDeliverDue_PermanentFailureMarksUndeliverable(t *testing.T) {
	now := streaktime.Now()
	svc, users, _, db := setupReminderSvc(t, now)
	u := createUser(t, users, "danghaiduong@1995", StartOfVNDay(now).Add(10*time.Minute))
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	mailer := &stubMailer{ready: true, sendErr: &email.Failure{Status: 422}}
	receipts := repository.NewEmailSendReceiptRepository(db)
	svc.AttachOutbound(mailer, receipts, nil, nil, "https://dadiary.vn/check-in")

	first, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.EmailSent != 0 || first.EmailFailed != 0 || first.EmailSkipped < 1 {
		t.Fatalf("permanent failure should suppress, not retry: %+v", first)
	}
	if mailer.Attempts() != 1 {
		t.Fatalf("attempts=%d", mailer.Attempts())
	}
	marked := logs.String()
	if !strings.Contains(marked, "email address marked undeliverable") {
		t.Fatalf("missing mark log: %s", marked)
	}
	if !strings.Contains(marked, u.ID.String()) {
		t.Fatalf("mark log missing user id: %s", marked)
	}
	if strings.Contains(marked, u.Email) {
		t.Fatalf("mark log contains full email: %s", marked)
	}

	got, err := users.GetByID(context.Background(), u.ID)
	if err != nil || got == nil || got.EmailReminderSuppressedAt == nil {
		t.Fatalf("user not marked undeliverable: %+v err=%v", got, err)
	}
	if got.EmailReminderFailCount != 1 || got.EmailReminderHash == "" {
		t.Fatalf("suppression state: %+v", got)
	}
	sent, err := receipts.HasSent(context.Background(), u.ID, "d0")
	if err != nil || sent {
		t.Fatalf("failed send must release claim: sent=%v err=%v", sent, err)
	}

	logs.Reset()
	second, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.EmailSent != 0 || second.EmailFailed != 0 {
		t.Fatalf("suppressed address retried: %+v", second)
	}
	if mailer.Attempts() != 1 {
		t.Fatalf("second run called Resend: attempts=%d", mailer.Attempts())
	}
	if strings.Contains(logs.String(), "email address marked undeliverable") {
		t.Fatalf("marked undeliverable more than once: %s", logs.String())
	}
}

func TestDeliverDue_TransientFailureKeepsRetrying(t *testing.T) {
	for _, status := range []int{500, 429, 0} {
		t.Run(httpStatusName(status), func(t *testing.T) {
			now := streaktime.Now()
			svc, users, _, db := setupReminderSvc(t, now)
			u := createUser(t, users, "retry@test.com", StartOfVNDay(now).Add(11*time.Minute))
			if _, err := svc.RefreshWindow(context.Background()); err != nil {
				t.Fatal(err)
			}
			var sendErr error = &email.Failure{Status: status}
			if status == 0 {
				sendErr = errors.New("dial tcp: connection reset")
			}
			mailer := &stubMailer{ready: true, sendErr: sendErr}
			receipts := repository.NewEmailSendReceiptRepository(db)
			svc.AttachOutbound(mailer, receipts, nil, nil, "https://dadiary.vn/check-in")

			first, err := svc.DeliverDue(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if first.EmailFailed < 1 || first.EmailSent != 0 {
				t.Fatalf("transient should fail open for retry: %+v", first)
			}
			got, err := users.GetByID(context.Background(), u.ID)
			if err != nil || got == nil {
				t.Fatal(err)
			}
			if got.EmailReminderSuppressedAt != nil || got.EmailReminderFailCount != 0 {
				t.Fatalf("transient must not suppress: %+v", got)
			}
			sent, err := receipts.HasSent(context.Background(), u.ID, "d0")
			if err != nil || sent {
				t.Fatalf("claim not released: sent=%v err=%v", sent, err)
			}

			second, err := svc.DeliverDue(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if second.EmailFailed < 1 || mailer.Attempts() != 2 {
				t.Fatalf("expected a second attempt: res=%+v attempts=%d", second, mailer.Attempts())
			}
		})
	}
}

func TestDeliverDue_AuthFailureDoesNotSuppress(t *testing.T) {
	now := streaktime.Now()
	svc, users, _, db := setupReminderSvc(t, now)
	u := createUser(t, users, "auth@test.com", StartOfVNDay(now).Add(9*time.Minute))
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	mailer := &stubMailer{ready: true, sendErr: &email.Failure{Status: 401}}
	svc.AttachOutbound(mailer, repository.NewEmailSendReceiptRepository(db), nil, nil, "https://dadiary.vn/check-in")

	for i := 0; i < 4; i++ {
		res, err := svc.DeliverDue(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if res.EmailFailed < 1 {
			t.Fatalf("run %d: %+v", i, res)
		}
	}
	if mailer.Attempts() != 4 {
		t.Fatalf("attempts=%d", mailer.Attempts())
	}
	got, err := users.GetByID(context.Background(), u.ID)
	if err != nil || got == nil || got.EmailReminderSuppressedAt != nil || got.EmailReminderFailCount != 0 {
		t.Fatalf("401 must not mark the address bad: %+v err=%v", got, err)
	}
}

func TestDeliverDue_RepeatedRejectionThenStops(t *testing.T) {
	now := streaktime.Now()
	svc, users, _, db := setupReminderSvc(t, now)
	u := createUser(t, users, "soft@test.com", StartOfVNDay(now).Add(8*time.Minute))
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	mailer := &stubMailer{ready: true, sendErr: &email.Failure{Status: 409}}
	svc.AttachOutbound(mailer, repository.NewEmailSendReceiptRepository(db), nil, nil, "https://dadiary.vn/check-in")

	for i := 1; i <= reminderEmailSuppressAfter; i++ {
		if _, err := svc.DeliverDue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if mailer.Attempts() != reminderEmailSuppressAfter {
		t.Fatalf("attempts=%d", mailer.Attempts())
	}
	got, err := users.GetByID(context.Background(), u.ID)
	if err != nil || got == nil || got.EmailReminderSuppressedAt == nil {
		t.Fatalf("expected suppression after %d rejections: %+v err=%v", reminderEmailSuppressAfter, got, err)
	}
	if _, err := svc.DeliverDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mailer.Attempts() != reminderEmailSuppressAfter {
		t.Fatalf("kept retrying after suppression: attempts=%d", mailer.Attempts())
	}
}

func TestDeliverDue_SuccessDoesNotSuppress(t *testing.T) {
	now := streaktime.Now()
	svc, users, _, db := setupReminderSvc(t, now)
	u := createUser(t, users, "ok@test.com", StartOfVNDay(now).Add(14*time.Minute))
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
	if first.EmailSent != 1 || first.EmailFailed != 0 || len(mailer.sent) != 1 {
		t.Fatalf("success path: %+v sent=%d", first, len(mailer.sent))
	}
	got, err := users.GetByID(context.Background(), u.ID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.EmailReminderSuppressedAt != nil || got.EmailReminderFailCount != 0 || got.EmailReminderHash != "" {
		t.Fatalf("success must not touch suppression: %+v", got)
	}
	sent, err := receipts.HasSent(context.Background(), u.ID, "d0")
	if err != nil || !sent {
		t.Fatalf("receipt missing: sent=%v err=%v", sent, err)
	}

	second, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.EmailSent != 0 || mailer.Attempts() != 1 {
		t.Fatalf("second run changed the success path: %+v attempts=%d", second, mailer.Attempts())
	}
}

func TestDeliverDue_SuccessClearsRejectionCount(t *testing.T) {
	now := streaktime.Now()
	svc, users, _, db := setupReminderSvc(t, now)
	u := createUser(t, users, "recover@test.com", StartOfVNDay(now).Add(6*time.Minute))
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	mailer := &stubMailer{ready: true, sendErr: &email.Failure{Status: 409}}
	svc.AttachOutbound(mailer, repository.NewEmailSendReceiptRepository(db), nil, nil, "https://dadiary.vn/check-in")

	for i := 0; i < reminderEmailSuppressAfter-1; i++ {
		if _, err := svc.DeliverDue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	mailer.sendErr = nil
	res, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.EmailSent != 1 {
		t.Fatalf("expected send after transient rejections: %+v", res)
	}
	got, err := users.GetByID(context.Background(), u.ID)
	if err != nil || got == nil || got.EmailReminderFailCount != 0 || got.EmailReminderHash != "" || got.EmailReminderSuppressedAt != nil {
		t.Fatalf("success should clear the rejection count: %+v err=%v", got, err)
	}
}

func TestDeliverDue_EmailChangeBecomesEligible(t *testing.T) {
	now := streaktime.Now()
	svc, users, _, db := setupReminderSvc(t, now)
	u := createUser(t, users, "old@1995", StartOfVNDay(now).Add(7*time.Minute))
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	mailer := &stubMailer{ready: true, sendErr: &email.Failure{Status: 422}}
	svc.AttachOutbound(mailer, repository.NewEmailSendReceiptRepository(db), nil, nil, "https://dadiary.vn/check-in")
	if _, err := svc.DeliverDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mailer.Attempts() != 1 {
		t.Fatalf("attempts=%d", mailer.Attempts())
	}

	if err := db.Model(&domain.User{}).Where("id = ?", u.ID).Update("email", "new@example.com").Error; err != nil {
		t.Fatal(err)
	}
	mailer.sendErr = nil
	res, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.EmailSent != 1 || mailer.Attempts() != 2 || len(mailer.sent) != 1 {
		t.Fatalf("updated address should send: %+v attempts=%d sent=%d", res, mailer.Attempts(), len(mailer.sent))
	}
	if mailer.sent[0].To != "new@example.com" {
		t.Fatalf("to=%q", mailer.sent[0].To)
	}
	got, err := users.GetByID(context.Background(), u.ID)
	if err != nil || got == nil || got.EmailReminderSuppressedAt != nil || got.EmailReminderHash != "" {
		t.Fatalf("suppression should clear after the new address: %+v err=%v", got, err)
	}
}

func httpStatusName(status int) string {
	if status == 0 {
		return "transport"
	}
	return "http_" + strings.TrimPrefix((&email.Failure{Status: status}).Error(), "email send failed: HTTP ")
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
