package dto

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
)

const maxAttributionRunes = 200

// Apply writes sanitized first-touch values onto a new user.
// A nil attribution leaves the columns unset. Values that contain "@"
// are dropped so an email address is never stored in these columns.
func (a *RegisterAttribution) Apply(user *domain.User) {
	if a == nil || user == nil {
		return
	}
	user.UTMSource = attributionPtr(a.UTMSource)
	user.UTMMedium = attributionPtr(a.UTMMedium)
	user.UTMCampaign = attributionPtr(a.UTMCampaign)
	user.UTMContent = attributionPtr(a.UTMContent)
	user.FBCLID = attributionPtr(a.FBCLID)
	user.TTCLID = attributionPtr(a.TTCLID)
}

func attributionPtr(raw string) *string {
	v := sanitizeAttribution(raw)
	if v == "" {
		return nil
	}
	return &v
}

// sanitizeAttribution trims, strips characters outside the campaign charset,
// and truncates to 200 runes. An "@" drops the whole value.
func sanitizeAttribution(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "@") {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		if attributionRuneOK(r) {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if utf8.RuneCountInString(out) <= maxAttributionRunes {
		return out
	}
	return string([]rune(out)[:maxAttributionRunes])
}

func attributionRuneOK(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	switch r {
	case '-', '_', '.', ':', '+', '/', '~', '%':
		return true
	default:
		return false
	}
}
