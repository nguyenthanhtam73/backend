package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_AppRefreshTTLDefault(t *testing.T) {
	t.Setenv("DADIARY_JWT_SECRET", "test-secret-for-app-refresh-ttl")
	if orig, ok := os.LookupEnv("DADIARY_JWT_APP_REFRESH_TTL"); ok {
		t.Cleanup(func() { _ = os.Setenv("DADIARY_JWT_APP_REFRESH_TTL", orig) })
	}
	if err := os.Unsetenv("DADIARY_JWT_APP_REFRESH_TTL"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(".env.does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWT.AppRefreshTTL != DefaultAppRefreshTTL {
		t.Fatalf("default app refresh ttl %s", cfg.JWT.AppRefreshTTL)
	}
	if cfg.JWT.AccessTTL != 24*time.Hour {
		t.Fatalf("access ttl %s", cfg.JWT.AccessTTL)
	}
	if cfg.JWT.RefreshTTL != 7*24*time.Hour {
		t.Fatalf("refresh ttl %s", cfg.JWT.RefreshTTL)
	}
}

func TestLoad_AppRefreshTTLFromEnv(t *testing.T) {
	t.Setenv("DADIARY_JWT_SECRET", "test-secret-for-app-refresh-ttl")
	t.Setenv("DADIARY_JWT_APP_REFRESH_TTL", "100h")
	cfg, err := Load(".env.does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWT.AppRefreshTTL != 100*time.Hour {
		t.Fatalf("env app refresh ttl %s", cfg.JWT.AppRefreshTTL)
	}
}
