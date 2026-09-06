package email

import (
	"strings"
	"testing"
)

func TestBuildReminderTemplate_NoDiagnosisClaims(t *testing.T) {
	banned := []string{"trị khỏi", "chẩn đoán bệnh", "cure", "diagnos"}
	for _, kind := range []Kind{KindD0, KindD1} {
		tpl := BuildReminderTemplate(kind, "https://dadiary.vn/check-in", "https://api/unsub", "Lan")
		if tpl.Subject == "" || tpl.Text == "" || tpl.HTML == "" {
			t.Fatalf("%s empty template", kind)
		}
		if !strings.Contains(tpl.Text, "https://dadiary.vn/check-in") {
			t.Fatalf("%s missing CTA", kind)
		}
		if !strings.Contains(tpl.Text, "https://api/unsub") {
			t.Fatalf("%s missing unsub", kind)
		}
		blob := strings.ToLower(tpl.Subject + tpl.Text + tpl.HTML)
		for _, w := range banned {
			if strings.Contains(blob, w) {
				t.Fatalf("%s contains banned %q", kind, w)
			}
		}
	}
	d0 := BuildReminderTemplate(KindD0, "", "", "")
	d1 := BuildReminderTemplate(KindD1, "", "", "")
	if d0.Subject == d1.Subject {
		t.Fatal("D0 and D1 subjects should differ")
	}
}

func TestUnsubscribeTokenRoundTrip(t *testing.T) {
	s := NewUnsubscribeSigner("test-secret", "https://api.dadiary.example")
	id := mustParseUUID("11111111-1111-1111-1111-111111111111")
	tok, err := s.Token(id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Parse(tok)
	if err != nil || got != id {
		t.Fatalf("got %s err=%v", got, err)
	}
	if _, err := s.Parse(tok + "x"); err == nil {
		t.Fatal("tampered token should fail")
	}
	u := s.URL(id)
	if !strings.Contains(u, "/api/v1/email/unsubscribe?token=") {
		t.Fatalf("url=%s", u)
	}
}
