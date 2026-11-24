package config

import "testing"

func TestLoad_VAPIDSubjectDefaultIsBareEmail(t *testing.T) {
	t.Setenv("DADIARY_JWT_SECRET", "test-secret-for-vapid-subject-default")
	t.Setenv("DADIARY_VAPID_SUBJECT", "")
	t.Setenv("DADIARY_VAPID_PUBLIC_KEY", "")
	t.Setenv("DADIARY_VAPID_PRIVATE_KEY", "")

	cfg, err := Load(".env.does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VAPID.Subject != "noreply@dadiary.app" {
		t.Fatalf("default subject %q", cfg.VAPID.Subject)
	}
	if cfg.VAPID.PublicKey != "" || cfg.VAPID.PrivateKey != "" {
		t.Fatal("default subject test must not require vapid keys")
	}
}
