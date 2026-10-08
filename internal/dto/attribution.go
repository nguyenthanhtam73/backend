package dto

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
)

// UTM values are sanitized, not matched against a strict ASCII pattern.
// Letters and digits may be any Unicode (so "Da mụn" is kept), including
// combining marks, so Vietnamese survives both NFC and NFD. The stored
// string is not re-normalized: NFC stays NFC and NFD stays NFD.
// Spaces and common punctuation are kept. "video_tu_do" is unchanged.
//
// A '+' or '%' that is query-string encoding is decoded first, so "Da+mụn"
// and "Da%20m%E1%BB%A5n" become "Da mụn". A literal '+' encoded as %2B stays.
// Control characters are stripped. Characters outside the allow-list
// (for example '*', a leftover '%', or an emoji) are removed and the rest
// is kept. Whitespace is trimmed. Internal spaces are not collapsed.
// Values are truncated to 100 runes. A value that contains '@' is dropped
// so an email is never stored. If nothing remains, the value is omitted.
//
// Click IDs stay ASCII: letters, digits, '.', '_', and '-'. Other
// characters are stripped and the rest is kept. fbclid allows 256 runes,
// matching the frontend truncation. ttclid allows 255. A result that is
// empty or longer than that cap is dropped. A value that contains '@' is
// dropped entirely. Click IDs are not query-decoded; a raw '+' is not a
// space here, it is simply not in the allow-list.

const (
	maxUTMRunes    = 100
	maxFBCLIDRunes = 256
	maxTTCLIDRunes = 255
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
	return acceptedClickID(raw, maxFBCLIDRunes)
}

func acceptedTTCLID(raw string) *string {
	return acceptedClickID(raw, maxTTCLIDRunes)
}

// acceptedClickID strips characters outside the ASCII click-id alphabet.
// A value that contains '@' is dropped whole. Empty and over-long results
// are omitted. The cap is a maximum, not a truncation: 257 fbclid runes
// are still dropped.
func acceptedClickID(raw string, maxRunes int) *string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "@") {
		return nil
	}
	cleaned := strings.Map(func(r rune) rune {
		if clickIDRune(r) {
			return r
		}
		return -1
	}, raw)
	if cleaned == "" || utf8.RuneCountInString(cleaned) > maxRunes {
		return nil
	}
	return &cleaned
}

func clickIDRune(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	case r == '.' || r == '_' || r == '-':
		return true
	default:
		return false
	}
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
	raw = strings.Map(func(r rune) rune {
		if allowedUTMRune(r) {
			return r
		}
		return -1
	}, raw)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
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
	// Mn keeps NFD Vietnamese (base letter + combining horn, dot below, …).
	// The string is not NFC-normalized; the marks are stored as received.
	if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || unicode.Is(unicode.Mn, r) {
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
