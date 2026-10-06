package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTransactionalSendIgnoresReminderOff locks the boundary between reminder
// jobs and account mail. Password reset, verify, and account-deletion mail
// use Sender.Send directly. That method has no user id and does not read
// users.reminder_enabled. Evening jobs filter reminder_enabled = false in
// their own candidate query.
func TestTransactionalSendIgnoresReminderOff(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &payload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"re_reset"}`))
	}))
	defer srv.Close()

	client := NewResendClient("re_test_key", "DaDiary <noreply@dadiary.vn>")
	client.endpoint = srv.URL
	id, err := client.Send(context.Background(), Message{
		To:      "off@example.com",
		Subject: "Đặt lại mật khẩu DaDiary",
		Text:    "Mở liên kết này để đặt lại mật khẩu. Nhắc da hàng ngày không đi qua thư này.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "re_reset" {
		t.Fatalf("id=%q", id)
	}
	if payload["subject"] != "Đặt lại mật khẩu DaDiary" {
		t.Fatalf("subject=%v", payload["subject"])
	}
	to, _ := payload["to"].([]any)
	if len(to) != 1 || to[0] != "off@example.com" {
		t.Fatalf("to=%v", payload["to"])
	}
}
