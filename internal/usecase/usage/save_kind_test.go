package usage

import "testing"

func TestIsPreferenceOnlySave(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind string
		want bool
	}{
		{"preference_only", true},
		{"PREFERENCE_ONLY", true},
		{"tick_only", false},
		{"manual_edit", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsPreferenceOnlySave(tc.kind); got != tc.want {
			t.Fatalf("IsPreferenceOnlySave(%q)=%v want %v", tc.kind, got, tc.want)
		}
	}
}
