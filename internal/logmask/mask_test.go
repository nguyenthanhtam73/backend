package logmask

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestMaskEmail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "normal", in: "thao@gmail.com", want: "t***@gmail.com"},
		{name: "short local", in: "a@gmail.com", want: "a***@gmail.com"},
		{name: "two char local", in: "ab@gmail.com", want: "a***@gmail.com"},
		{name: "no at", in: "not-an-email", want: "***"},
		{name: "empty", in: "", want: "***"},
		{name: "whitespace", in: "   ", want: "***"},
		{name: "uppercase", in: "Thao@Gmail.COM", want: "T***@Gmail.COM"},
		{name: "plus-addressing", in: "thao.nguyen+ads@gmail.com", want: "t***@gmail.com"},
		{name: "empty local", in: "@gmail.com", want: "***"},
		{name: "empty domain", in: "thao@", want: "***"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MaskEmail(tc.in)
			if got != tc.want {
				t.Fatalf("MaskEmail(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(tc.in, "+") && strings.Contains(got, "+") {
				t.Fatalf("plus-tag leaked: %q", got)
			}
		})
	}
}

func TestMaskIP(t *testing.T) {
	if got := MaskIP("203.0.113.44"); got != "203.0.113.0" {
		t.Fatalf("v4 = %q", got)
	}
	if got := MaskIP("203.0.113.44:443"); got != "203.0.113.0" {
		t.Fatalf("v4 port = %q", got)
	}
	if got := MaskIP("2001:db8:1234:5678::1"); got != "2001:db8:1234::" {
		t.Fatalf("v6 = %q", got)
	}
	if got := MaskIP("[2001:db8:1234:5678::1]:443"); got != "2001:db8:1234::" {
		t.Fatalf("v6 port = %q", got)
	}
	if got := MaskIP("not-an-ip"); got != "***" {
		t.Fatalf("bad = %q", got)
	}
	if got := MaskIP(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
	list := MaskIPList("203.0.113.44, 2001:db8:1234:5678::9")
	if list != "203.0.113.0, 2001:db8:1234::" {
		t.Fatalf("list = %q", list)
	}
}

func TestRedactEmbeddedSecrets(t *testing.T) {
	raw := `duplicate Key (email)=(thao.nguyen+ads@gmail.com) password=$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy ` +
		`Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U ` +
		`Cookie: session=abc "user_note":"da đỏ hôm nay" "password":"password1" ` +
		`/uploads/checks/a.jpg 2026/09/30/check-in/thao__11111111-1111-1111-1111-111111111111/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.jpg ` +
		`sk-live-secretkey12345 re_Abcd1234 spsk_live_secret`
	got := Redact(raw)
	for _, leak := range []string{
		"thao.nguyen",
		"password1",
		"$2a$10$",
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		"session=abc",
		"da đỏ",
		"/uploads/checks/a.jpg",
		"thao__11111111",
		"sk-live-secretkey12345",
		"re_Abcd1234",
		"spsk_live_secret",
	} {
		if strings.Contains(got, leak) {
			t.Fatalf("redacted text still has %q\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "t***@gmail.com") {
		t.Fatalf("masked email missing: %s", got)
	}
	again := Redact(got)
	if again != got {
		t.Fatalf("redact not idempotent\nfirst:  %s\nsecond: %s", got, again)
	}
}

func TestRedactSQLRegisterInsert(t *testing.T) {
	const email = "thao.nguyen+ads@gmail.com"
	sql := `INSERT INTO "users" ("email","password_hash","username") VALUES ('` + email + `','$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy','thao')`
	got := RedactSQL(sql)
	if strings.Contains(got, email) || strings.Contains(got, "thao.nguyen") || strings.Contains(got, "$2a$") {
		t.Fatalf("register SQL leaked: %s", got)
	}
	if !strings.Contains(got, "'***'") {
		t.Fatalf("literals not replaced: %s", got)
	}
}

func TestRedactSQLCheckInNote(t *testing.T) {
	sql := `INSERT INTO "skin_checks" ("user_note","image_urls") VALUES ('da mình đỏ và ngứa','["2026/09/30/check-in/lan__uuid/photo.jpg"]')`
	got := RedactSQL(sql)
	if strings.Contains(got, "ngứa") || strings.Contains(got, "photo.jpg") || strings.Contains(got, "lan__") {
		t.Fatalf("check-in SQL leaked: %s", got)
	}
}

func TestHandlerRedactsErrorAttribute(t *testing.T) {
	var buf bytes.Buffer
	h := NewHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger := slog.New(h)
	err := errors.New("unique violation for thao.nguyen+ads@gmail.com")
	logger.Error("auth: register failed", "err", err, "email", "thao.nguyen+ads@gmail.com")
	got := buf.String()
	if strings.Contains(got, "thao.nguyen") {
		t.Fatalf("handler leaked email: %s", got)
	}
	if !strings.Contains(got, "t***@gmail.com") {
		t.Fatalf("handler missing mask: %s", got)
	}
}
