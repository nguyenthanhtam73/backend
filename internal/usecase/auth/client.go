package auth

import (
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// ClientHeader is the preferred Android session marker.
// Only the exact value "android" selects the long app refresh TTL.
const ClientHeader = "X-DaDiary-Client"

// ResolveClientKind prefers a non-empty ClientHeader. When the header is
// absent, the JSON "client" field is used. "android" and "android-app"
// (the value the native app sends on every request) are app sessions.
// Anything else is a web session.
func ResolveClientKind(headerValue, bodyValue string) string {
	if header := strings.TrimSpace(headerValue); header != "" {
		return domain.NormalizeRefreshClient(header)
	}
	return domain.NormalizeRefreshClient(bodyValue)
}
