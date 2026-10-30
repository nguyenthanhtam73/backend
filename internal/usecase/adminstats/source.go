package adminstats

import "strings"

const (
	sourceTikTok   = "tiktok"
	sourceFacebook = "facebook"
	sourceOther    = "other"
	sourceNone     = "none"
)

// facebookUTMSources are exact utm_source values (after trim + lower) that
// count as Facebook when no TikTok signal is present. "meta" is not in this
// set; a Meta click is facebook only when fbclid is stored.
var facebookUTMSources = map[string]struct{}{
	"fb":        {},
	"facebook":  {},
	"ig":        {},
	"instagram": {},
}

// classifyRegistrationSource buckets one signup.
//
// TikTok wins when ttclid is non-blank or utm_source contains "tiktok"
// (case-insensitive), even if fbclid is also set. Facebook is next: fbclid
// non-blank, or utm_source exactly fb / facebook / ig / instagram. Any other
// non-blank utm_source is other. Blank source and blank click ids are none.
func classifyRegistrationSource(utmSource, fbclid, ttclid *string) string {
	source := trimmed(utmSource)
	if clickIDPresent(ttclid) || strings.Contains(strings.ToLower(source), "tiktok") {
		return sourceTikTok
	}
	if clickIDPresent(fbclid) || facebookUTMSource(source) {
		return sourceFacebook
	}
	if source != "" {
		return sourceOther
	}
	return sourceNone
}

func facebookUTMSource(source string) bool {
	_, ok := facebookUTMSources[strings.ToLower(source)]
	return ok
}

func clickIDPresent(v *string) bool {
	return trimmed(v) != ""
}

func trimmed(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
