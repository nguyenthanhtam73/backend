package checkinreminder

import (
	"context"
	"testing"

	"github.com/dadiary/backend/internal/repository"
)

func TestDeliverEveningEmails_SkipsReminderOffKeepsNullAndTrue(t *testing.T) {
	now := vnAt(2026, 10, 5, 19, 30)
	svc, users, checks, db := setupReminderSvc(t, now)
	unset := createUser(t, users, "unset@test.com", vnAt(2026, 9, 1, 9, 0))
	on := createUser(t, users, "on@test.com", vnAt(2026, 9, 1, 9, 0))
	off := createUser(t, users, "off@test.com", vnAt(2026, 9, 1, 9, 0))
	enabled := true
	disabled := false
	if err := users.SetReminderSchedule(context.Background(), on.ID, &enabled, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := users.SetReminderSchedule(context.Background(), off.ID, &disabled, nil, nil); err != nil {
		t.Fatal(err)
	}
	insertCheck(t, checks, unset, vnAt(2026, 10, 4, 20, 0))
	insertCheck(t, checks, on, vnAt(2026, 10, 4, 20, 0))
	insertCheck(t, checks, off, vnAt(2026, 10, 4, 20, 0))

	mailer := &stubMailer{ready: true}
	svc.AttachOutbound(mailer, repository.NewEmailSendReceiptRepository(db), nil, nil, "https://dadiary.vn/check-in")
	res, err := svc.DeliverEveningEmails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.EmailSent != 2 || len(mailer.sent) != 2 {
		t.Fatalf("evening send: %+v emails=%d", res, len(mailer.sent))
	}
	got := map[string]bool{}
	for _, msg := range mailer.sent {
		got[msg.To] = true
	}
	if !got["unset@test.com"] || !got["on@test.com"] || got["off@test.com"] {
		t.Fatalf("recipients: %#v", got)
	}
}
