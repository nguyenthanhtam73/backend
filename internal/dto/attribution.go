package dto

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
)

// UTM values are sanitized, not matched against a strict ASCII pattern.
// Letters and digits may be any Unicode (so "Da mụn" is kept). Spaces and
// common punctuation are kept. "video_tu_do" is unchanged.
//
// A '+' or '%' that is query-string encoding is decoded first, so "Da+mụn"
// and "Da%20m%E1%BB%A5n" become "Da mụn". A literal '+' encoded as %2B stays.
// Control characters are stripped. Whitespace is trimmed. Values are
// truncated to 100 runes. A value that contains '@' is dropped so an email
// is never stored.
//
// Click IDs stay ASCII. fbclid allows 256 characters, matching the frontend
// truncation. ttclid allows 255. '.' is allowed because TikTok ttclid values
// use it. A miss is dropped.

const maxUTMRunes = 100

var (
	fbclidPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,256}$`)
	ttclidPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)
)

// Apply writes first-touch values that pass validation onto a new user.
// A nil attribution leaves the columns unset. A dropped value is stored as
// NULL, and the rest of the register still succeeds.
func (a *RegisterAttribution) Apply(user *domain.User) {
	if a == nil || user == nil {
		return
	}
	user.UTMSource = acceptedUTM(a.UTMSource)
	user.UTMMedium = acceptedUTM(a.UTMMedium)
	user.UTMCampaign = acceptedUTM(a.UTMCampaign)
	user.UTMContent = acceptedUTM(a.UTMContent)
	user.FBCLID = acceptedFBCLID(a.FBCLID)
	user.TTCLID = acceptedTTCLID(a.TTCLID)
}

func acceptedUTM(raw string) *string {
	cleaned := sanitizeUTM(raw)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func acceptedFBCLID(raw string) *string {
	return acceptedMatch(raw, fbclidPattern)
}

func acceptedTTCLID(raw string) *string {
	return acceptedMatch(raw, ttclidPattern)
}

func acceptedMatch(raw string, pattern *regexp.Regexp) *string {
	if !pattern.MatchString(raw) {
		return nil
	}
	v := raw
	return &v
}

// sanitizeUTM returns the value to store, or "" when the value must be dropped.
func sanitizeUTM(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = decodeQueryAttribution(raw)
	raw = stripControlChars(raw)
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "@") {
		return ""
	}
	for _, r := range raw {
		if !allowedUTMRune(r) {
			return ""
		}
	}
	if utf8.RuneCountInString(raw) > maxUTMRunes {
		raw = string([]rune(raw)[:maxUTMRunes])
		raw = strings.TrimSpace(raw)
	}
	return raw
}

// decodeQueryAttribution turns query-string encoding into text.
// QueryUnescape maps a raw '+' to a space and decodes %XX, so %2B stays '+'.
// When the value is not valid percent-encoding, a raw '+' is still a space.
func decodeQueryAttribution(raw string) string {
	if !strings.ContainsAny(raw, "+%") {
		return raw
	}
	if decoded, err := url.QueryUnescape(raw); err == nil {
		return decoded
	}
	return strings.ReplaceAll(raw, "+", " ")
}

func stripControlChars(raw string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, raw)
}

func allowedUTMRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
		return true
	}
	switch r {
	case '.', '_', '-', ',', ':', ';', '\'', '"', '/', '(', ')', '[', ']',
		'!', '?', '&', '=', '~', '#', '|', '+':
		return true
	default:
		return false
	}
}
