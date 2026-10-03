package config

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/clientip"
)

func TestLoadTrustedProxyDefaults(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_HEADER", "")
	t.Setenv("DADIARY_TRUSTED_PROXY_HEADER", "")
	t.Setenv("TRUSTED_PROXIES", "")
	t.Setenv("DADIARY_TRUSTED_PROXIES", "")

	cfg, err := Load(".env")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.TrustedProxyHeader != clientip.DefaultHeader {
		t.Fatalf("header = %q", cfg.HTTP.TrustedProxyHeader)
	}
	if strings.Join(cfg.HTTP.TrustedProxies, ",") != clientip.DefaultProxies {
		t.Fatalf("proxies = %#v", cfg.HTTP.TrustedProxies)
	}
}

func TestLoadTrustedProxyEnvOverride(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_HEADER", "X-Real-IP")
	t.Setenv("TRUSTED_PROXIES", "100.64.0.2, 127.0.0.1")

	cfg, err := Load(".env")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.TrustedProxyHeader != "X-Real-IP" {
		t.Fatalf("header = %q", cfg.HTTP.TrustedProxyHeader)
	}
	if strings.Join(cfg.HTTP.TrustedProxies, ",") != "100.64.0.2,127.0.0.1" {
		t.Fatalf("proxies = %#v", cfg.HTTP.TrustedProxies)
	}
}

func TestLoadTrustedProxyRejectsBadCIDR(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "everywhere")
	_, err := Load(".env")
	if err == nil || !strings.Contains(err.Error(), "trusted proxy") {
		t.Fatalf("expected invalid trusted proxy, got %v", err)
	}
}
