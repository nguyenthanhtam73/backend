package auth

import (
	"testing"

	"github.com/dadiary/backend/internal/domain"
)

func TestResolveClientKind(t *testing.T) {
	cases := []struct {
		header, body, want string
	}{
		{"android", "", domain.RefreshClientAndroid},
		{"android", "web", domain.RefreshClientAndroid},
		{"  android  ", "ios", domain.RefreshClientAndroid},
		{"", "android", domain.RefreshClientAndroid},
		{"", "  android  ", domain.RefreshClientAndroid},
		{"web", "android", domain.RefreshClientWeb},
		{"Android", "android", domain.RefreshClientWeb},
		{"ANDROID", "", domain.RefreshClientWeb},
		{"ios", "", domain.RefreshClientWeb},
		{"", "ios", domain.RefreshClientWeb},
		{"", "Android", domain.RefreshClientWeb},
		{"", "", domain.RefreshClientWeb},
	}
	for _, tc := range cases {
		got := ResolveClientKind(tc.header, tc.body)
		if got != tc.want {
			t.Errorf("header %q body %q = %q, want %q", tc.header, tc.body, got, tc.want)
		}
	}
}
