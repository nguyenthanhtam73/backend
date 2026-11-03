package handler

import (
	"strings"
	"time"

	"github.com/dadiary/backend/internal/lobby"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// accessCookieDomain is the registrable domain so a page on dadiary.vn
// and a socket on api.dadiary.vn share the access JWT. Same-site (not
// same-origin) is enough for SameSite=Lax to attach the cookie.
const accessCookieDomain = "dadiary.vn"

// setAccessCookie stores the access JWT in dadiary_access_token.
// expiry is when that same access token expires. The value is never logged.
func setAccessCookie(c *fiber.Ctx, accessToken string, expiry time.Time) {
	if c == nil {
		return
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" || expiry.IsZero() {
		return
	}
	sec := int(time.Until(expiry).Seconds())
	if sec < 1 {
		sec = 1
	}
	c.Cookie(&fiber.Cookie{
		Name:     lobby.AccessCookie,
		Value:    accessToken,
		Path:     "/",
		Domain:   accessCookieDomain,
		MaxAge:   sec,
		Expires:  expiry.UTC(),
		Secure:   true,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}

// clearAccessCookie expires dadiary_access_token. Attributes match setAccessCookie
// so the browser replaces the cookie that was stored for dadiary.vn.
func clearAccessCookie(c *fiber.Ctx) {
	if c == nil {
		return
	}
	c.Cookie(&fiber.Cookie{
		Name:     lobby.AccessCookie,
		Value:    "",
		Path:     "/",
		Domain:   accessCookieDomain,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		Secure:   true,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}

// bearerToken is the raw access JWT from Authorization. Empty when the
// header is missing or not Bearer. Callers must not log the result.
func bearerToken(c *fiber.Ctx) string {
	if c == nil {
		return ""
	}
	header := strings.TrimSpace(c.Get(fiber.HeaderAuthorization))
	if len(header) < 7 || !strings.EqualFold(header[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(header[7:])
}

// accessExpiry reads exp from an access JWT that the auth middleware already
// verified. The signature is not checked again here, and nothing is logged.
func accessExpiry(raw string) (time.Time, bool) {
	claims := &jwt.RegisteredClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(strings.TrimSpace(raw), claims); err != nil {
		return time.Time{}, false
	}
	if claims.ExpiresAt == nil {
		return time.Time{}, false
	}
	return claims.ExpiresAt.Time, true
}
