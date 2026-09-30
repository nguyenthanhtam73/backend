// Package logmask redacts personal data before it is written to logs.
// Stored rows and outbound mail are out of scope — call these only at log sites.
package logmask

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaskEmail keeps the first character of the local part, then "***", then the
// full domain: "thao@gmail.com" → "t***@gmail.com". Plus-tags stay hidden
// because only that first character is kept. Empty input, a missing "@", or an
// empty local part or domain becomes "***".
func MaskEmail(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "***"
	}
	at := strings.LastIndex(addr, "@")
	if at <= 0 || at == len(addr)-1 {
		return "***"
	}
	local := addr[:at]
	r, size := utf8.DecodeRuneInString(local)
	if r == utf8.RuneError || size <= 0 {
		return "***"
	}
	return string(r) + "***" + addr[at:]
}

// MaskIP shortens an address for logs.
//
// IPv4 drops the last octet (203.0.113.44 → 203.0.113.0) so a /24 remains
// for abuse correlation without storing a host address. IPv6 keeps the first
// 48 bits and zeros the rest, which is the usual customer-site prefix and
// matches how we treat the v4 last octet. Unparseable input becomes "***".
// An empty string stays empty.
func MaskIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	host := raw
	if h, _, err := net.SplitHostPort(raw); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if ip == nil {
		return "***"
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.0", v4[0], v4[1], v4[2])
	}
	v6 := ip.To16()
	if v6 == nil {
		return "***"
	}
	masked := make(net.IP, net.IPv6len)
	copy(masked[:6], v6[:6])
	return masked.String()
}

// MaskIPList masks each address in a comma-separated header value (X-Forwarded-For).
func MaskIPList(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, MaskIP(p))
	}
	return strings.Join(out, ", ")
}

var (
	jsonSecretRE   = regexp.MustCompile(`(?i)"(password|password_hash|passwd|refresh_token|access_token|api_key|secret|authorization|cookie|user_note|environment_note|summary_notes|situation_analysis|notes|overview|image_urls|photo_urls|analysis|endpoint|p256dh)"\s*:\s*"(?:[^"\\]|\\.)*"`)
	formSecretRE   = regexp.MustCompile(`(?i)(password|passwd|refresh_token|access_token|api_key|secret|authorization|cookie|user_note|summary_notes)=([^&\s]+)`)
	authHeaderRE   = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)\S+`)
	cookieHeaderRE = regexp.MustCompile(`(?i)(cookie\s*[:=]\s*)[^;\s]+(?:\s*;\s*[^;\s]+)*`)
	bearerRE       = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._\-+/=]+`)
	jwtRE          = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	bcryptRE       = regexp.MustCompile(`\$2[aby]\$\d{2}\$[./A-Za-z0-9]{53}`)
	apiKeyRE       = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_\-]{10,}|re_[A-Za-z0-9]{8,}|spsk_(?:live|test)_[A-Za-z0-9]{4,})\b`)
	uploadRE       = regexp.MustCompile(`(?i)(?:https?://[^\s"'<>]+)?/uploads/[^\s"'<>\\]+`)
	photoKeyRE     = regexp.MustCompile(`\d{4}/\d{2}/\d{2}/(?:check-in|onboarding|admin-skin-review(?:-public)?)/[^\s"'<>\\]+`)
	emailRE        = regexp.MustCompile(`(?i)[a-z0-9](?:[a-z0-9._%+\-]{0,62}[a-z0-9])?@[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?)*\.[a-z]{2,}`)
	sensitiveSQL   = regexp.MustCompile(`(?i)(?:"|\b)(?:email|password(?:_hash)?|user_note|environment_note|summary_notes|image_urls|photo_urls|skin_scores|strengths|improvements|routine_hints|product_suggestions|avoid_or_patch|safety_flags|refresh_token|token_hash|p256dh|climate_context|situation_analysis|endpoint|analysis)(?:"|\b)`)
)

// Redact masks emails and strips other secrets and check-in text from a free-form
// log string (error text, SQL, a response body). It is safe to call more than once.
func Redact(s string) string {
	if s == "" {
		return s
	}
	s = jsonSecretRE.ReplaceAllString(s, `"$1":"***"`)
	s = formSecretRE.ReplaceAllString(s, `$1=***`)
	s = authHeaderRE.ReplaceAllString(s, `${1}***`)
	s = cookieHeaderRE.ReplaceAllString(s, `${1}***`)
	s = bearerRE.ReplaceAllString(s, "Bearer ***")
	s = jwtRE.ReplaceAllString(s, "***")
	s = bcryptRE.ReplaceAllString(s, "***")
	s = apiKeyRE.ReplaceAllString(s, "***")
	s = uploadRE.ReplaceAllString(s, "/uploads/***")
	s = photoKeyRE.ReplaceAllString(s, "***")
	s = emailRE.ReplaceAllStringFunc(s, MaskEmail)
	return s
}

// RedactSQL redacts a GORM/Postgres statement. When the statement names a
// sensitive column, every string literal is replaced so notes, photo keys, and
// hashes cannot survive as quoted values the email matcher would leave alone.
func RedactSQL(sql string) string {
	out := Redact(sql)
	if sql == "" || !sensitiveSQL.MatchString(sql) {
		return out
	}
	return redactSQLStringLiterals(out)
}

func redactSQLStringLiterals(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	for i := 0; i < len(sql); i++ {
		if sql[i] != '\'' {
			b.WriteByte(sql[i])
			continue
		}
		j := i + 1
		for j < len(sql) {
			if sql[j] == '\'' {
				if j+1 < len(sql) && sql[j+1] == '\'' {
					j += 2
					continue
				}
				break
			}
			j++
		}
		b.WriteString("'***'")
		if j >= len(sql) {
			break
		}
		i = j
	}
	return b.String()
}
