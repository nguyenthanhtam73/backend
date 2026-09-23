package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResendClient_NotConfigured(t *testing.T) {
	c := NewResendClient("", "")
	if c.Configured() {
		t.Fatal("empty client should not be configured")
	}
	err := c.Send(context.Background(), Message{To: "a@b.com", Subject: "s", Text: "t"})
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
	err := c.Send(context.Background(), Message{
		To:          "user@example.com",
		Subject:     "Streak da chưa mở… chụp 1 tấm là xong ✨",
		Text:        "body",
		HTML:        "<p>body</p>",
		Unsubscribe: "https://api.example/unsub",
	})
	if err != nil {
		t.Fatal(err)
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
