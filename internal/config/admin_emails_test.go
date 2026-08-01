package config

import "testing"

func TestCanSkinReviewEmail(t *testing.T) {
	cfg := &Config{
		AdminEmails:      []string{"full@example.com"},
		SkinReviewEmails: []string{"reviewer@example.com"},
	}
	cases := []struct {
		email   string
		admin   bool
		canSkin bool
		opOnly  bool
	}{
		{"full@example.com", true, true, false},
		{"FULL@example.com", true, true, false},
		{"reviewer@example.com", false, true, true},
		{"nobody@example.com", false, false, false},
		{"", false, false, false},
	}
	for _, tc := range cases {
		if got := cfg.IsAdminEmail(tc.email); got != tc.admin {
			t.Fatalf("IsAdminEmail(%q)=%v want %v", tc.email, got, tc.admin)
		}
		if got := cfg.CanSkinReviewEmail(tc.email); got != tc.canSkin {
			t.Fatalf("CanSkinReviewEmail(%q)=%v want %v", tc.email, got, tc.canSkin)
		}
		if got := cfg.IsSkinReviewOperatorEmail(tc.email); got != tc.opOnly {
			t.Fatalf("IsSkinReviewOperatorEmail(%q)=%v want %v", tc.email, got, tc.opOnly)
		}
	}
}

func TestNormalizeEmailListFromEnvFallback(t *testing.T) {
	got := normalizeEmailList(nil, " A@X.com , b@y.com ")
	if len(got) != 2 || got[0] != "a@x.com" || got[1] != "b@y.com" {
		t.Fatalf("unexpected list: %#v", got)
	}
	got = normalizeEmailList([]string{"Keep@Me.com"}, "ignored@x.com")
	if len(got) != 1 || got[0] != "keep@me.com" {
		t.Fatalf("existing list should win: %#v", got)
	}
}
