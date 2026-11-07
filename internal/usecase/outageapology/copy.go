package outageapology

import (
	"fmt"
	"html"
	"strings"
	"time"
)

const (
	// Subject is the exact Vietnamese subject line.
	Subject = "DaDiary xin lỗi vì kết quả soi da của bạn bị chậm"

	// BodyPrefix is the shared apology. The variant sentence is appended after the trailing space.
	BodyPrefix = "Chào bạn,\nTừ tối 6/11 tới tối 7/11, hệ thống AI tự động của DaDiary bị lỗi kỹ thuật nên chưa phân tích được ảnh bạn gửi. Ảnh của bạn vẫn được giữ an toàn và riêng tư. Tụi mình rất xin lỗi bạn.\nGiờ lỗi đã sửa xong. "

	HasResultSentence = "Kết quả của bạn đã có, mời bạn vào xem nhé."
	RecheckSentence   = "Mời bạn vào soi da lại, chỉ mất một phút thôi."

	HasResultButton = "Xem kết quả của tôi"
	RecheckButton   = "Soi da lại"

	// Signature is the exact sign-off.
	Signature = "DaDiary — Nhật ký da · Trợ lý của riêng bạn"

	// ReceiptKind is the one-shot send marker (at most one email per user).
	ReceiptKind = "ai_outage_apology_20261106"
)

// Window is the outage cohort: checks created from 2026-11-06 13:00Z through 2026-11-07 18:05Z, inclusive.
var (
	WindowStart = time.Date(2026, 11, 6, 13, 0, 0, 0, time.UTC)
	WindowEnd   = time.Date(2026, 11, 7, 18, 5, 0, 0, time.UTC)
)

// Message is one rendered apology.
type Message struct {
	Subject string
	Text    string
	HTML    string
	Button  string
	URL     string
}

// Build renders the has-result or re-check variant. origin is DADIARY_PUBLIC_WEB_URL.
// checkID empty selects the re-check button and /check-in.
func Build(origin, checkID string) Message {
	base := strings.TrimRight(strings.TrimSpace(origin), "/")
	if base == "" {
		base = "https://dadiary.vn"
	}
	hasResult := strings.TrimSpace(checkID) != ""
	sentence := RecheckSentence
	button := RecheckButton
	link := base + "/check-in"
	if hasResult {
		sentence = HasResultSentence
		button = HasResultButton
		link = base + "/skin-checks/" + strings.TrimSpace(checkID)
	}
	body := BodyPrefix + sentence
	text := body + "\n\n" + button + ": " + link + "\n\n" + Signature
	return Message{
		Subject: Subject,
		Text:    text,
		HTML:    htmlBody(body, button, link),
		Button:  button,
		URL:     link,
	}
}

func htmlBody(body, button, link string) string {
	paragraphs := strings.Split(body, "\n")
	var b strings.Builder
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		fmt.Fprintf(&b, `<p style="margin:0 0 12px;font-size:16px;line-height:1.6;color:#334155;">%s</p>`, html.EscapeString(p))
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="vi"><body style="margin:0;padding:24px;background:#FAFDFB;font-family:Arial,Helvetica,sans-serif;color:#334155;">
  <table role="presentation" width="100%%" cellspacing="0" cellpadding="0"><tr><td align="center">
    <table role="presentation" width="100%%" style="max-width:520px;background:#FFFFFF;border-radius:16px;padding:28px;">
      <tr><td style="font-size:13px;letter-spacing:0.08em;text-transform:uppercase;color:#134E4A;">DaDiary</td></tr>
      <tr><td style="padding-top:16px;">%s</td></tr>
      <tr><td style="padding-top:8px;"><a href="%s" style="display:inline-block;background:#2DD4BF;color:#0F766E;text-decoration:none;padding:12px 22px;border-radius:999px;font-size:15px;">%s</a></td></tr>
      <tr><td style="padding-top:20px;font-size:13px;color:#64748B;">%s</td></tr>
    </table>
  </td></tr></table>
</body></html>`, b.String(), html.EscapeString(link), html.EscapeString(button), html.EscapeString(Signature))
}
