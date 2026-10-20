package config

import (
	"os"
	"testing"
)

func TestLoad_AIPrimaryProvider(t *testing.T) {
	tests := []struct {
		name    string
		present bool
		value   string
		want    string
	}{
		{name: "unset", present: false, want: ""},
		{name: "openai", present: true, value: "openai", want: "openai"},
		{name: "anthropic", present: true, value: "anthropic", want: "anthropic"},
		{name: "invalid", present: true, value: "not-a-provider", want: "not-a-provider"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The prefixed name must not choose the provider.
			t.Setenv("DADIARY_AI_PRIMARY_PROVIDER", "anthropic")
			if tc.present {
				t.Setenv("AI_PRIMARY_PROVIDER", tc.value)
			} else {
				t.Setenv("AI_PRIMARY_PROVIDER", "sentinel")
				os.Unsetenv("AI_PRIMARY_PROVIDER")
			}
			cfg, err := Load(".env")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.AI.PrimaryProvider != tc.want {
				t.Fatalf("PrimaryProvider = %q, want %q", cfg.AI.PrimaryProvider, tc.want)
			}
		})
	}
}
