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

// reminderFontStack is system faces that include Vietnamese glyphs.
// Do not load a web font: missing glyphs render as boxes in mail clients.
const reminderFontStack = "Arial, Helvetica, Roboto, 'Noto Sans', sans-serif"

// Reminder colors are the light CapCut mint palette. Title stays deep teal;
// body and muted lines are slate so the mail does not read as heavy teal.
// Button label is #0F766E: white on #2DD4BF is about 1.9:1, #0F766E is about 2.9:1.
const (
	reminderPageBackground = "#FAFDFB"
	reminderCardBackground = "#FFFFFF"
	reminderTitleColor     = "#134E4A"
	reminderBodyColor      = "#334155"
	reminderMutedColor     = "#64748B"
	reminderButtonColor    = "#2DD4BF"
	reminderButtonText     = "#0F766E"
)

func reminderStyle(rest string) string {
	style := "font-family:" + reminderFontStack
	if rest != "" {
		style += ";" + rest
	}
	return style
}

// BuildReminderTemplate returns Vietnamese D0/D1 copy (Duo-style, no diagnosis).
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
		subject = "DaDiary ghé hỏi — trời đổi, hôm nay check-in chưa?"
		greeting = fmt.Sprintf("Chào %s,", name)
		body = "Thời tiết đổi, da cần được quan tâm hơn. Nhắc nhẹ thôi, không mắng đâu."
		button = "Check-in da hôm nay"
	default:
		subject = "Streak da chưa mở… trời đổi, chụp 1 tấm là xong ✨"
		greeting = fmt.Sprintf("Chào %s,", name)
		body = "Ê, DaDiary đã sẵn rồi. Thời tiết đổi, da cần được quan tâm hơn. Một tấm ảnh thôi — không cần đẹp."
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
			`<p style="%s">Nếu bạn không muốn nhận email nhắc từ DaDiary, <a href="%s" style="%s">hủy đăng ký tại đây</a>.</p>`,
			reminderStyle("margin:24px 0 0;font-size:12px;color:"+reminderMutedColor+";line-height:1.5;"),
			html.EscapeString(unsub),
			reminderStyle("color:"+reminderMutedColor+";"),
		)
	}

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html lang="vi">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="%s">
  <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="%s">
    <tr><td align="center" style="%s">
      <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="%s">
        <tr><td style="%s">DaDiary</td></tr>
        <tr><td style="%s">%s</td></tr>
        <tr><td style="%s">%s</td></tr>
        <tr><td style="%s">
          <a href="%s" style="%s">%s</a>
        </td></tr>
        <tr><td style="%s">Hoặc mở: %s</td></tr>
        %s
      </table>
    </td></tr>
  </table>
</body>
</html>`,
		reminderStyle("margin:0;padding:0;background:"+reminderPageBackground+";color:"+reminderBodyColor+";"),
		reminderStyle("background:"+reminderPageBackground+";padding:32px 16px;"),
		reminderStyle(""),
		reminderStyle("max-width:520px;background:"+reminderCardBackground+";border-radius:16px;padding:32px 28px;"),
		reminderStyle("font-size:13px;letter-spacing:0.08em;text-transform:uppercase;color:"+reminderTitleColor+";"),
		reminderStyle("padding-top:16px;font-size:22px;line-height:1.35;color:"+reminderTitleColor+";"),
		safeGreeting,
		reminderStyle("padding-top:16px;font-size:16px;line-height:1.6;color:"+reminderBodyColor+";"),
		safeBody,
		reminderStyle("padding-top:28px;"),
		safeCTA,
		reminderStyle("display:inline-block;background:"+reminderButtonColor+";color:"+reminderButtonText+";text-decoration:none;padding:12px 22px;border-radius:999px;font-size:15px;"),
		safeButton,
		reminderStyle("padding-top:20px;font-size:13px;color:"+reminderMutedColor+";"),
		safeCTA,
		unsubHTML,
	)

	return Template{Subject: subject, Text: text, HTML: htmlBody}
}
