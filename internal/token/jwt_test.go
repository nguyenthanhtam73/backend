package token

import (
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
)

func TestNewService_MissingSecretFailsClosed(t *testing.T) {
	for _, secret := range []string{"", "   ", "\t"} {
		svc, err := NewService(config.JWTConfig{
			Secret:     secret,
			AccessTTL:  time.Hour,
			RefreshTTL: 24 * time.Hour,
		})
		if err == nil || svc != nil {
			t.Fatalf("secret %q: svc=%v err=%v", secret, svc, err)
		}
		if !strings.Contains(err.Error(), "jwt secret is empty") {
			t.Fatalf("err=%v", err)
		}
		if strings.Contains(err.Error(), secret) && strings.TrimSpace(secret) != "" {
			t.Fatalf("error leaked the secret: %v", err)
		}
	}
}
