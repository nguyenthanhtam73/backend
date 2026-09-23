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
	if d0.Subject != "Streak da chưa mở… trời đổi, chụp 1 tấm là xong ✨" {
		t.Fatalf("d0 subject=%q", d0.Subject)
	}
	if !strings.Contains(d0.Text, "Ê, DaDiary đã sẵn rồi. Thời tiết đổi, da cần được quan tâm hơn. Một tấm ảnh thôi — không cần đẹp.") {
		t.Fatalf("d0 body: %s", d0.Text)
	}
	if !strings.Contains(d0.Text, "Chào bạn,") || !strings.Contains(d0.HTML, "https://dadiary.vn/check-in") {
		t.Fatalf("d0 greeting/cta: %s", d0.Text)
	}
	if d1.Subject != "DaDiary ghé hỏi — trời đổi, hôm nay check-in chưa?" {
		t.Fatalf("d1 subject=%q", d1.Subject)
	}
	if !strings.Contains(d1.Text, "Thời tiết đổi, da cần được quan tâm hơn. Nhắc nhẹ thôi, không mắng đâu.") {
		t.Fatalf("d1 body: %s", d1.Text)
	}
}

func TestBuildReminderTemplate_UsesVietnameseSystemFont(t *testing.T) {
	for _, kind := range []Kind{KindD0, KindD1} {
		tpl := BuildReminderTemplate(kind, "https://dadiary.vn/check-in", "https://api/unsub", "Lan")
		htmlBody := tpl.HTML
		if strings.Contains(htmlBody, "Georgia") || strings.Contains(htmlBody, "@font-face") || strings.Contains(htmlBody, "fonts.google") {
			t.Fatalf("%s loads a non-system font", kind)
		}
		if strings.Count(htmlBody, "font-family:"+reminderFontStack) < 10 {
			t.Fatalf("%s font stack not applied to body and text nodes:\n%s", kind, htmlBody)
		}
		if !strings.Contains(htmlBody, `font-family:`+reminderFontStack) {
			t.Fatalf("%s missing font stack", kind)
		}
	}
}

func TestBuildReminderTemplate_UsesMintTealBrand(t *testing.T) {
	required := []string{"#FAFDFB", "#FFFFFF", "#134E4A", "#334155", "#64748B", "#2DD4BF", "#0F766E"}
	banned := []string{
		"#faf6f2", "#FAF6F2",
		"#c4785a", "#C4785A",
		"#c4a484", "#C4A484",
		"#3d342e", "#5c524c", "#8a7f78",
		"#F3FAF7", "#14B8A6", "#4C7672",
		"#0D9488", "#5E7A76",
	}
	for _, kind := range []Kind{KindD0, KindD1} {
		htmlBody := BuildReminderTemplate(kind, "https://dadiary.vn/check-in", "https://api/unsub", "Lan").HTML
		for _, hex := range required {
			if !strings.Contains(htmlBody, hex) {
				t.Fatalf("%s missing brand color %s", kind, hex)
			}
		}
		if !strings.Contains(htmlBody, "background:#2DD4BF;color:#0F766E") || !strings.Contains(htmlBody, "border-radius:999px") {
			t.Fatalf("%s CTA is not a #2DD4BF pill with #0F766E label", kind)
		}
		if strings.Contains(htmlBody, "color:#FFFFFF") {
			t.Fatalf("%s still uses white text", kind)
		}
		if !strings.Contains(htmlBody, "font-size:22px;line-height:1.35;color:#134E4A") {
			t.Fatalf("%s title is not #134E4A", kind)
		}
		if !strings.Contains(htmlBody, "font-size:16px;line-height:1.6;color:#334155") {
			t.Fatalf("%s body text is not #334155", kind)
		}
		for _, old := range banned {
			if strings.Contains(htmlBody, old) {
				t.Fatalf("%s still uses %s", kind, old)
			}
		}
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
