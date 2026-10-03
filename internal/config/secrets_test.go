package config

import (
	"strings"
	"testing"
)

const (
	defaultJWTSecret = "change-me-in-production-use-long-random-string"
	goodJWTSecret    = "jwt-secret-at-least-32-bytes-long!!"
	goodMediaKey     = "media-key-at-least-32-bytes-long!!"
)

func TestValidateStartupSecrets(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		jwt     string
		media   string
		wantSub string
	}{
		{
			name:    "prod default jwt secret",
			env:     "production",
			jwt:     defaultJWTSecret,
			media:   goodMediaKey,
			wantSub: "DADIARY_JWT_SECRET is a known placeholder",
		},
		{
			name:    "prod short media key",
			env:     "production",
			jwt:     goodJWTSecret,
			media:   "short-media-key",
			wantSub: "DADIARY_MEDIA_SIGNING_KEY is shorter than 32 bytes",
		},
		{
			name:    "prod missing media key",
			env:     "production",
			jwt:     goodJWTSecret,
			media:   "",
			wantSub: "DADIARY_MEDIA_SIGNING_KEY is required in production",
		},
		{
			name:  "prod good keys",
			env:   "production",
			jwt:   goodJWTSecret,
			media: goodMediaKey,
		},
		{
			name:  "dev defaults",
			env:   "development",
			jwt:   defaultJWTSecret,
			media: "",
		},
		{
			name:    "prod media key equals jwt secret",
			env:     "Production",
			jwt:     goodJWTSecret,
			media:   goodJWTSecret,
			wantSub: "DADIARY_MEDIA_SIGNING_KEY must not equal DADIARY_JWT_SECRET",
		},
		{
			name:    "prod short jwt secret",
			env:     "production",
			jwt:     "too-short",
			media:   goodMediaKey,
			wantSub: "DADIARY_JWT_SECRET is shorter than 32 bytes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateStartupSecrets(&Config{
				Env: tc.env,
				JWT: JWTConfig{Secret: tc.jwt},
				Media: MediaConfig{
					SigningKey: tc.media,
				},
			})
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("expected start to succeed, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected refusing to start")
			}
			msg := err.Error()
			if !strings.Contains(msg, "fatal: refusing to start:") || !strings.Contains(msg, tc.wantSub) {
				t.Fatalf("error %q does not contain %q", msg, tc.wantSub)
			}
			for _, secret := range []string{tc.jwt, tc.media, defaultJWTSecret} {
				if secret != "" && strings.Contains(msg, secret) {
					t.Fatalf("error leaked a secret value: %q", msg)
				}
			}
		})
	}
}
