package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// loadCookieHeader reads a cookie file and returns a Cookie request header value.
//
// Supported formats:
//   - One line: name=value; name2=value2  (paste from DevTools → Network → Cookie)
//   - One cookie per line: name=value
//   - JSON array: [{"name":"SPC_F","value":"..."}, ...]  (EditThisCookie / Cookie-Editor export)
//   - Netscape cookie file (# Netscape HTTP Cookie File)
func loadCookieHeader(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(b))
	if text == "" {
		return "", fmt.Errorf("cookie file is empty")
	}

	if strings.HasPrefix(text, "[") {
		return parseJSONCookies(text)
	}
	if strings.HasPrefix(text, "# Netscape HTTP Cookie File") || strings.Contains(text, "\t") {
		return parseNetscapeCookies(text)
	}
	if strings.Contains(text, ";") && !strings.Contains(text, "\n") {
		return normalizeCookieHeader(text), nil
	}

	var parts []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			parts = append(parts, line)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("could not parse cookie file")
	}
	return strings.Join(parts, "; "), nil
}

func parseJSONCookies(text string) (string, error) {
	var rows []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(text), &rows); err != nil {
		return "", err
	}
	var parts []string
	for _, r := range rows {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		parts = append(parts, name+"="+strings.TrimSpace(r.Value))
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("no cookies in JSON export")
	}
	return strings.Join(parts, "; "), nil
}

func parseNetscapeCookies(text string) (string, error) {
	var parts []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		domain := fields[0]
		if !strings.Contains(domain, "shopee.vn") {
			continue
		}
		name, value := fields[5], fields[6]
		if name == "" {
			continue
		}
		parts = append(parts, name+"="+value)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("no shopee.vn cookies in Netscape file")
	}
	return strings.Join(parts, "; "), nil
}

func normalizeCookieHeader(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "cookie:") {
		s = strings.TrimSpace(s[7:])
	}
	return s
}

func applyCookieHeader(req *http.Request, cookieHeader string) {
	req.Header.Set("Cookie", cookieHeader)
	if csrf := cookieValue(cookieHeader, "csrftoken"); csrf != "" {
		req.Header.Set("X-CSRFToken", csrf)
	}
	if csrf := cookieValue(cookieHeader, "SPC_CDS"); csrf != "" {
		req.Header.Set("X-CSRFToken", csrf)
	}
}

func cookieValue(header, name string) string {
	prefix := name + "="
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, prefix) {
			return strings.TrimPrefix(part, prefix)
		}
	}
	return ""
}
