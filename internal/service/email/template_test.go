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
	if d0.Subject != "Streak da chưa mở… chụp 1 tấm là xong ✨" {
		t.Fatalf("d0 subject=%q", d0.Subject)
	}
	if !strings.Contains(d0.Text, "Ê, DaDiary đã sẵn rồi. Chưa check-in thì streak đang chờ mở. Một tấm ảnh thôi — không cần đẹp.") {
		t.Fatalf("d0 body: %s", d0.Text)
	}
	if !strings.Contains(d0.Text, "Chào bạn,") || !strings.Contains(d0.HTML, "https://dadiary.vn/check-in") {
		t.Fatalf("d0 greeting/cta: %s", d0.Text)
	}
	if d1.Subject != "DaDiary ghé hỏi — hôm nay check-in chưa?" {
		t.Fatalf("d1 subject=%q", d1.Subject)
	}
	if !strings.Contains(d1.Text, "Hôm nay thiếu 1 tấm check-in là tiếc. Nhắc nhẹ thôi, không mắng đâu.") {
		t.Fatalf("d1 body: %s", d1.Text)
	}
}

func TestBuildReminderTemplate_KeepsNameAndUnsubscribe(t *testing.T) {
	tpl := BuildReminderTemplate(KindD0, "https://dadiary.vn/check-in", "https://api/unsub?token=abc", "Lan")
	if !strings.Contains(tpl.Text, "Chào Lan,") {
		t.Fatalf("greeting: %s", tpl.Text)
	}
	if !strings.Contains(tpl.HTML, `href="https://dadiary.vn/check-in"`) {
		t.Fatal("html button must keep check-in URL")
	}
	if !strings.Contains(tpl.HTML, "hủy đăng ký tại đây") || !strings.Contains(tpl.HTML, "https://api/unsub?token=abc") {
		t.Fatal("html unsubscribe missing")
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
