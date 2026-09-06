package email

import (
	"fmt"
	"html"
	"strings"
)

// Kind is a D0/D1 reminder email.
type Kind string

const (
	KindD0 Kind = "d0"
	KindD1 Kind = "d1"
)

// Template is rendered copy for one reminder email.
type Template struct {
	Subject string
	Text    string
	HTML    string
}

// BuildReminderTemplate returns Vietnamese D0/D1 copy (soft tone, no diagnosis).
func BuildReminderTemplate(kind Kind, checkInURL, unsubURL, displayName string) Template {
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "bạn"
	}
	cta := strings.TrimSpace(checkInURL)
	if cta == "" {
		cta = "https://dadiary.vn/check-in"
	}
	unsub := strings.TrimSpace(unsubURL)

	var subject, greeting, body, button string
	switch kind {
	case KindD1:
		subject = "DaDiary nhớ bạn — check-in nhẹ một phút thôi"
		greeting = fmt.Sprintf("Chào %s,", name)
		body = "Hôm qua bạn đã mở DaDiary. Hôm nay chụp một tấm check-in da nhé — chỉ một phút, không cần hoàn hảo. Mình nhắc nhẹ để đồng hành cùng bạn thôi."
		button = "Check-in da hôm nay"
	default:
		subject = "Hôm nay chụp một tấm check-in da nhé"
		greeting = fmt.Sprintf("Chào %s,", name)
		body = "Chào mừng bạn đến DaDiary. Dành một phút chụp check-in da hôm nay nhé — chỉ cần ánh sáng đều và một tấm ảnh. Mình nhắc nhẹ thôi."
		button = "Check-in da ngay"
	}

	footer := "Bạn nhận email này vì đã tạo tài khoản DaDiary."
	if unsub != "" {
		footer += " Nếu không muốn nhận email nhắc, nhấn: " + unsub
	}

	text := strings.Join([]string{
		greeting,
		"",
		body,
		"",
		button + ": " + cta,
		"",
		footer,
	}, "\n")

	safeGreeting := html.EscapeString(greeting)
	safeBody := html.EscapeString(body)
	safeButton := html.EscapeString(button)
	safeCTA := html.EscapeString(cta)
	unsubHTML := ""
	if unsub != "" {
		unsubHTML = fmt.Sprintf(
			`<p style="margin:24px 0 0;font-size:12px;color:#8a7f78;line-height:1.5;">Nếu bạn không muốn nhận email nhắc từ DaDiary, <a href="%s" style="color:#8a7f78;">hủy đăng ký tại đây</a>.</p>`,
			html.EscapeString(unsub),
		)
	}

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html lang="vi">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:#faf6f2;font-family:Georgia,serif;color:#3d342e;">
  <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="background:#faf6f2;padding:32px 16px;">
    <tr><td align="center">
      <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="max-width:520px;background:#fff;border-radius:16px;padding:32px 28px;">
        <tr><td style="font-size:13px;letter-spacing:0.08em;text-transform:uppercase;color:#c4a484;">DaDiary</td></tr>
        <tr><td style="padding-top:16px;font-size:22px;line-height:1.35;">%s</td></tr>
        <tr><td style="padding-top:16px;font-size:16px;line-height:1.6;color:#5c524c;">%s</td></tr>
        <tr><td style="padding-top:28px;">
          <a href="%s" style="display:inline-block;background:#c4785a;color:#fff;text-decoration:none;padding:12px 22px;border-radius:999px;font-size:15px;">%s</a>
        </td></tr>
        <tr><td style="padding-top:20px;font-size:13px;color:#8a7f78;">Hoặc mở: %s</td></tr>
        %s
      </table>
    </td></tr>
  </table>
</body>
</html>`, safeGreeting, safeBody, safeCTA, safeButton, safeCTA, unsubHTML)

	return Template{Subject: subject, Text: text, HTML: htmlBody}
}
