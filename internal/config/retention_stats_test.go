package config

import "testing"

func TestRetentionStatsExcludedEmailSubstrings(t *testing.T) {
	want := []string{
		"+goalacne1003",
		"+nolabel1003",
		"+fe53test1003",
		"+dadiarytest0925",
	}
	if len(RetentionStatsExcludedEmailSubstrings) != len(want) {
		t.Fatalf("markers=%v", RetentionStatsExcludedEmailSubstrings)
	}
	for i, m := range want {
		if RetentionStatsExcludedEmailSubstrings[i] != m {
			t.Fatalf("marker[%d]=%q want %q", i, RetentionStatsExcludedEmailSubstrings[i], m)
		}
	}
}

func TestRetentionStatsExcludedEmails(t *testing.T) {
	if got := RetentionStatsExcludedEmails(nil); len(got) != 0 {
		t.Fatalf("nil cfg: %#v", got)
	}
	cfg := &Config{
		AdminEmails:      []string{" Admin@DaDiary.vn ", "admin@dadiary.vn"},
		SkinReviewEmails: []string{"reviewer@dadiary.vn", "admin@dadiary.vn"},
	}
	got := RetentionStatsExcludedEmails(cfg)
	want := []string{"admin@dadiary.vn", "reviewer@dadiary.vn"}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v want %#v", got, want)
		}
	}
}
