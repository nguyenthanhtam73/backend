package payment

import (
	"net/url"
	"strings"

	"github.com/dadiary/backend/internal/config"
)

// localizeCallbackURLs returns success / error / cancel URLs for the UI locale.
//
// next-intl uses localePrefix "as-needed" + defaultLocale "vi":
//   - vi → https://dadiary.vn/payment/success
//   - en → https://dadiary.vn/en/payment/success
func localizeCallbackURLs(cfg config.SePayConfig, locale string) (success, errURL, cancel string) {
	loc := normalizeUILocale(locale)
	web := strings.TrimRight(strings.TrimSpace(cfg.PublicWebURL), "/")
	if web == "" {
		web = originFromURL(cfg.SuccessURL)
	}

	if web != "" {
		prefix := localePathPrefix(loc)
		return web + prefix + "/payment/success",
			web + prefix + "/payment/error",
			web + prefix + "/payment/cancel"
	}

	// Fallback: rewrite configured absolute URLs in place.
	return withLocalePrefix(cfg.SuccessURL, loc),
		withLocalePrefix(cfg.ErrorURL, loc),
		withLocalePrefix(cfg.CancelURL, loc)
}

func normalizeUILocale(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "en", "en-us", "en-gb":
		return "en"
	default:
		return "vi"
	}
}

func localePathPrefix(locale string) string {
	if locale == "en" {
		return "/en"
	}
	return ""
}

func originFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host, "/")
}

// withLocalePrefix injects /en (or strips it for vi) before /payment/...
func withLocalePrefix(raw, locale string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return raw
	}
	path := u.Path
	// Strip existing locale segment.
	for _, p := range []string{"/en/", "/vi/"} {
		if strings.HasPrefix(path, p) {
			path = "/" + strings.TrimPrefix(path, p)
			break
		}
	}
	for _, p := range []string{"/en", "/vi"} {
		if path == p {
			path = "/"
			break
		}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if locale == "en" {
		if path == "/" {
			path = "/en"
		} else {
			path = "/en" + path
		}
	}
	u.Path = path
	return u.String()
}
