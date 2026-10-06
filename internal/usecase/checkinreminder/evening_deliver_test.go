package checkinreminder

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/email"
	"github.com/dadiary/backend/internal/streaktime"
)

func insertCheck(t *testing.T, checks *repository.GormSkinCheckRepository, user *domain.User, day time.Time) {
	t.Helper()
	row := &domain.SkinCheck{
		UserID:    user.ID,
		ImageURLs: json.RawMessage(`[]`),
		CheckDate: streaktime.DateOf(day),
	}
	if err := checks.CreateWithAnalysis(context.Background(), row, &domain.SkinAnalysis{}); err != nil {
		t.Fatal(err)
	}
}

func TestDeliverDue_D0StillSendsAndD1EmailWaitsForEvening(t *testing.T) {
	now := vnAt(2026, 10, 5, 0, 30)
	svc, users, _, db := setupReminderSvc(t, now)
	createUser(t, users, "d0midnight@test.com", vnAt(2026, 10, 5, 0, 10))
	createUser(t, users, "signupyday@test.com", vnAt(2026, 10, 4, 12, 0))
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	mailer := &stubMailer{ready: true}
	svc.AttachOutbound(mailer, repository.NewEmailSendReceiptRepository(db), nil, nil, "https://dadiary.vn/check-in")

	res, err := svc.DeliverDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.EmailSent != 1 || len(mailer.sent) != 1 {
		t.Fatalf("hourly should send only D0: sent=%d emails=%d result=%+v", res.EmailSent, len(mailer.sent), res)
	}
	if strings.Contains(mailer.sent[0].Text, "src=") {
		t.Fatalf("D0 CTA should stay bare: %s", mailer.sent[0].Text)
	}
	if !strings.Contains(mailer.sent[0].Text, "Streak da chưa mở") && !strings.Contains(mailer.sent[0].Subject, "Streak da chưa mở") {
		t.Fatalf("expected D0 copy, subject=%q", mailer.sent[0].Subject)
	}
}

func TestDeliverEveningEmails_D1AfterFirstCheckIn(t *testing.T) {
	now := vnAt(2026, 10, 5, 19, 30)
	svc, users, checks, db := setupReminderSvc(t, now)
	// Signed up long before; first check-in was yesterday. Not a signup-D1.
	u := createUser(t, users, "first@test.com", vnAt(2026, 9, 1, 9, 0))
	insertCheck(t, checks, u, vnAt(2026, 10, 4, 20, 0))

	mailer := &stubMailer{ready: true}
	receipts := repository.NewEmailSendReceiptRepository(db)
	svc.AttachOutbound(mailer, receipts, nil, email.NewUnsubscribeSigner("secret", "https://api.test"), "https://dadiary.vn/check-in")

	first, err := svc.DeliverEveningEmails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.EmailSent != 1 || len(mailer.sent) != 1 {
		t.Fatalf("d1 send: %+v emails=%d", first, len(mailer.sent))
	}
	if !strings.Contains(mailer.sent[0].Text, "https://dadiary.vn/check-in?src=email_d1") ||
		!strings.Contains(mailer.sent[0].HTML, "src=email_d1") {
		t.Fatalf("d1 cta: %s", mailer.sent[0].Text)
	}
	if !strings.Contains(mailer.sent[0].Text, "Nhắc nhẹ thôi, không mắng đâu.") {
		t.Fatalf("d1 copy changed: %s", mailer.sent[0].Text)
	}
	var rec domain.EmailSendReceipt
	if err := db.Where("user_id = ? AND kind = ?", u.ID, "d1").First(&rec).Error; err != nil {
		t.Fatal(err)
	}
	if rec.ResendEmailID != "re_stub" {
		t.Fatalf("resend id=%q", rec.ResendEmailID)
	}

	second, err := svc.DeliverEveningEmails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.EmailSent != 0 || len(mailer.sent) != 1 {
		t.Fatalf("idempotent evening: %+v emails=%d", second, len(mailer.sent))
	}
}

func TestDeliverEveningEmails_SkipsWhenCheckedInToday(t *testing.T) {
	now := vnAt(2026, 10, 5, 19, 45)
	svc, users, checks, db := setupReminderSvc(t, now)
	u := createUser(t, users, "back@test.com", vnAt(2026, 10, 4, 8, 0))
	insertCheck(t, checks, u, vnAt(2026, 10, 4, 8, 0))
	insertCheck(t, checks, u, vnAt(2026, 10, 5, 18, 0))
	mailer := &stubMailer{ready: true}
	svc.AttachOutbound(mailer, repository.NewEmailSendReceiptRepository(db), nil, nil, "https://dadiary.vn/check-in")
	res, err := svc.DeliverEveningEmails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.EmailSent != 0 || len(mailer.sent) != 0 {
		t.Fatalf("already checked in: %+v", res)
	}
}

func TestDeliverEveningEmails_D3OnlyWithoutDay2Return(t *testing.T) {
	now := vnAt(2026, 10, 5, 19, 30)
	svc, users, checks, db := setupReminderSvc(t, now)
	missed := createUser(t, users, "missed@test.com", vnAt(2026, 9, 1, 9, 0))
	returned := createUser(t, users, "returned@test.com", vnAt(2026, 9, 1, 9, 0))
	// First check-in Oct 2 → day 2 is Oct 4, day 3 email is Oct 5.
	insertCheck(t, checks, missed, vnAt(2026, 10, 2, 11, 0))
	insertCheck(t, checks, returned, vnAt(2026, 10, 2, 11, 0))
	insertCheck(t, checks, returned, vnAt(2026, 10, 4, 12, 0))

	mailer := &stubMailer{ready: true}
	svc.AttachOutbound(mailer, repository.NewEmailSendReceiptRepository(db), nil, nil, "https://dadiary.vn/check-in")
	res, err := svc.DeliverEveningEmails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.EmailSent != 1 || len(mailer.sent) != 1 {
		t.Fatalf("d3: %+v emails=%d", res, len(mailer.sent))
	}
	if mailer.sent[0].To != "missed@test.com" {
		t.Fatalf("sent to %s", mailer.sent[0].To)
	}
	if !strings.Contains(mailer.sent[0].Text, "src=email_d3") ||
		!strings.Contains(mailer.sent[0].HTML, "src=email_d3") {
		t.Fatalf("d3 cta: %s", mailer.sent[0].Text)
	}
	if !strings.Contains(mailer.sent[0].Text, "Ba hôm trước bạn đã check-in.") {
		t.Fatalf("d3 copy: %s", mailer.sent[0].Text)
	}
}

func TestDeliverEveningEmails_SkipsDeletedUser(t *testing.T) {
	now := vnAt(2026, 10, 5, 19, 30)
	svc, users, checks, db := setupReminderSvc(t, now)
	u := createUser(t, users, "gone@test.com", vnAt(2026, 9, 1, 9, 0))
	insertCheck(t, checks, u, vnAt(2026, 10, 4, 20, 0))
	if err := db.Unscoped().Where("id = ?", u.ID).Delete(&domain.User{}).Error; err != nil {
		t.Fatal(err)
	}
	mailer := &stubMailer{ready: true}
	svc.AttachOutbound(mailer, repository.NewEmailSendReceiptRepository(db), nil, nil, "https://dadiary.vn/check-in")

	res, err := svc.DeliverEveningEmails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.EmailSent != 0 || len(mailer.sent) != 0 {
		t.Fatalf("sent to a deleted user: %+v emails=%d", res, len(mailer.sent))
	}
	if res.EmailSkipped < 1 {
		t.Fatalf("missing user was not skipped: %+v", res)
	}
	var n int64
	if err := db.Model(&domain.User{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("deleted user recreated: %d", n)
	}
	var receipts int64
	if err := db.Model(&domain.EmailSendReceipt{}).Count(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatalf("receipt created for a deleted user: %d", receipts)
	}
}
