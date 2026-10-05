package email

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResendClient_NotConfigured(t *testing.T) {
	c := NewResendClient("", "")
	if c.Configured() {
		t.Fatal("empty client should not be configured")
	}
	_, err := c.Send(context.Background(), Message{To: "a@b.com", Subject: "s", Text: "t"})
	if err != ErrNotConfigured {
		t.Fatalf("err=%v", err)
	}
}

func TestResendClient_SendPostsJSON(t *testing.T) {
	var gotAuth string
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"re_test"}`))
	}))
	defer srv.Close()

	c := NewResendClient("re_test_key", "DaDiary <noreply@dadiary.vn>")
	c.endpoint = srv.URL
	id, err := c.Send(context.Background(), Message{
		To:          "user@example.com",
		Subject:     "Streak da chưa mở… trời đổi, chụp 1 tấm là xong ✨",
		Text:        "body",
		HTML:        "<p>body</p>",
		Unsubscribe: "https://api.example/unsub",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "re_test" {
		t.Fatalf("resend id=%q", id)
	}
	if gotAuth != "Bearer re_test_key" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if payload["from"] != "DaDiary <noreply@dadiary.vn>" {
		t.Fatalf("from=%v", payload["from"])
	}
	headers, _ := payload["headers"].(map[string]any)
	if headers["List-Unsubscribe"] != "<https://api.example/unsub>" {
		t.Fatalf("headers=%v", headers)
	}
}

func TestResendClient_ClassifiesHTTPStatus(t *testing.T) {
	cases := []struct {
		status int
		class  FailureClass
	}{
		{http.StatusUnprocessableEntity, FailurePermanent},
		{http.StatusBadRequest, FailurePermanent},
		{http.StatusInternalServerError, FailureTransient},
		{http.StatusTooManyRequests, FailureTransient},
		{http.StatusUnauthorized, FailureTransient},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"name":"validation_error","message":"Invalid to field."}`))
		}))
		c := NewResendClient("re_test_key", "DaDiary <noreply@dadiary.vn>")
		c.endpoint = srv.URL
		_, err := c.Send(context.Background(), Message{
			To:      "danghaiduong@1995",
			Subject: "subject",
			Text:    "body",
		})
		srv.Close()
		if !errors.Is(err, ErrSendFailed) {
			t.Fatalf("status %d err=%v", tc.status, err)
		}
		if Classify(err) != tc.class {
			t.Fatalf("status %d class=%v want %v", tc.status, Classify(err), tc.class)
		}
		if strings.Contains(err.Error(), "danghaiduong@1995") {
			t.Fatalf("error includes mailbox: %v", err)
		}
	}

	fromSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"name":"validation_error","message":"Invalid ` + "`from`" + ` field. The email address needs to follow the email@example.com format."}`))
	}))
	defer fromSrv.Close()
	fromClient := NewResendClient("re_test_key", "DaDiary <noreply@dadiary.vn>")
	fromClient.endpoint = fromSrv.URL
	_, fromErr := fromClient.Send(context.Background(), Message{To: "user@example.com", Subject: "s", Text: "t"})
	if Classify(fromErr) != FailureRejection {
		t.Fatalf("invalid from class=%v err=%v", Classify(fromErr), fromErr)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := srv.URL
	srv.Close()
	c := NewResendClient("re_test_key", "DaDiary <noreply@dadiary.vn>")
	c.endpoint = endpoint
	_, err := c.Send(context.Background(), Message{To: "a@b.com", Subject: "s", Text: "t"})
	if Classify(err) != FailureTransient {
		t.Fatalf("transport class=%v err=%v", Classify(err), err)
	}
}
