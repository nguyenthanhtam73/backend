package handler

import (
	"strings"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/gofiber/fiber/v2"
)

// Header sent by the Android trial app and by the game page inside its WebView.
// Web requests that omit it stay on the admin allow-list.
const (
	mascotClientHeader   = "X-DaDiary-Client"
	mascotAndroidClient  = "android-app"
	mascotClosetAdminKey = "mascot_closet_admin"
)

// androidTrialClient reports the exact trial client value.
// The header name is case-insensitive. The value is not.
func androidTrialClient(c *fiber.Ctx) bool {
	if c == nil {
		return false
	}
	return strings.TrimSpace(c.Get(mascotClientHeader)) == mascotAndroidClient
}

// mascotOpenFor is the Bé Da gate. Admins and the Android trial client may
// enter. Other signed-in web requests stay closed while MascotOpenToEveryone
// is false. The trial header does not grant admin-only closet pieces.
func mascotOpenFor(c *fiber.Ctx, cfg *config.Config, email string) bool {
	admin := cfg != nil && cfg.IsAdminEmail(email)
	return domain.MascotVisibleFor(admin, androidTrialClient(c))
}

func mascotOpen(c *fiber.Ctx, cfg *config.Config) bool {
	return mascotOpenFor(c, cfg, middleware.UserEmailFromLocals(c))
}
