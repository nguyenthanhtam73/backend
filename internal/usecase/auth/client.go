package auth

import (
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// ClientHeader is the preferred Android session marker.
// Only the exact value "android" selects the long app refresh TTL.
const ClientHeader = "X-DaDiary-Client"

// ResolveClientKind prefers a non-empty ClientHeader. When the header is
// absent, the JSON "client" field is used. Anything other than the exact
// value android is a web session.
func ResolveClientKind(headerValue, bodyValue string) string {
	if header := strings.TrimSpace(headerValue); header != "" {
		return domain.NormalizeRefreshClient(header)
	}
	return domain.NormalizeRefreshClient(bodyValue)
}
