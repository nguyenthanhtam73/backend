package dto

import (
	"regexp"

	"github.com/dadiary/backend/internal/domain"
)

// UTM values must match the whole string: ^[A-Za-z0-9_.-]{1,100}$.
// Click IDs match ^[A-Za-z0-9_.-]{1,255}$. '.' is allowed because TikTok
// ttclid values use it. Meta fbclid values from the landing URL are letters,
// digits, '_' and '-' and still match. A miss is dropped, not rewritten.

var (
	utmValuePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	clickIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)
)

// Apply writes first-touch values that pass validation onto a new user.
// A nil attribution leaves the columns unset. A value that fails its pattern
// is stored as NULL, and the rest of the register still succeeds.
func (a *RegisterAttribution) Apply(user *domain.User) {
	if a == nil || user == nil {
		return
	}
	user.UTMSource = acceptedUTM(a.UTMSource)
	user.UTMMedium = acceptedUTM(a.UTMMedium)
	user.UTMCampaign = acceptedUTM(a.UTMCampaign)
	user.UTMContent = acceptedUTM(a.UTMContent)
	user.FBCLID = acceptedClickID(a.FBCLID)
	user.TTCLID = acceptedClickID(a.TTCLID)
}

func acceptedUTM(raw string) *string {
	return acceptedMatch(raw, utmValuePattern)
}

func acceptedClickID(raw string) *string {
	return acceptedMatch(raw, clickIDPattern)
}

func acceptedMatch(raw string, pattern *regexp.Regexp) *string {
	if !pattern.MatchString(raw) {
		return nil
	}
	v := raw
	return &v
}
