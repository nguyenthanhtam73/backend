package dto

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
)

func TestSanitizeUTM(t *testing.T) {
	long := strings.Repeat("x", 150)
	viet := strings.Repeat("ạ", 150)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "vietnamese ad name", in: "Da mụn", want: "Da mụn"},
		{name: "meta content token", in: "video_tu_do", want: "video_tu_do"},
		{name: "email dropped", in: "a@b.com", want: ""},
		{name: "truncated to 100 runes", in: long, want: strings.Repeat("x", 100)},
		{name: "truncated vietnamese runes", in: viet, want: strings.Repeat("ạ", 100)},
		{name: "control chars stripped", in: "\x00Da\x07 mụn\n", want: "Da mụn"},
		{name: "plus from query encoding", in: "Da+mụn", want: "Da mụn"},
		{name: "percent encoding", in: "Da%20m%E1%BB%A5n", want: "Da mụn"},
		{name: "encoded literal plus kept", in: "Da%2Bmụn", want: "Da+mụn"},
		{name: "trimmed", in: "  video_tu_do  ", want: "video_tu_do"},
		{name: "html dropped", in: "ig<script>", want: ""},
		{name: "percent-encoded email dropped", in: "a%40b.com", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeUTM(tc.in)
			if got != tc.want {
				t.Fatalf("sanitizeUTM(%q)=%q want %q", tc.in, got, tc.want)
			}
			if tc.want != "" && utf8.RuneCountInString(got) > 100 {
				t.Fatalf("len runes=%d", utf8.RuneCountInString(got))
			}
		})
	}
}

func TestAttributionApply_SanitizesUTMAndClickIDs(t *testing.T) {
	user := &domain.User{}
	(&RegisterAttribution{
		UTMSource:   "a@b.com",
		UTMMedium:   "paid social",
		UTMCampaign: "Da mụn",
		UTMContent:  "video_tu_do",
		FBCLID:      "IwAR0abc_def-123",
		TTCLID:      "E.C.P.abc_DEF-123",
	}).Apply(user)

	if user.UTMSource != nil {
		t.Fatalf("email utm_source stored: %s", *user.UTMSource)
	}
	if user.UTMMedium == nil || *user.UTMMedium != "paid social" {
		t.Fatalf("utm_medium=%v", user.UTMMedium)
	}
	if user.UTMCampaign == nil || *user.UTMCampaign != "Da mụn" {
		t.Fatalf("utm_campaign=%v", user.UTMCampaign)
	}
	if user.UTMContent == nil || *user.UTMContent != "video_tu_do" {
		t.Fatalf("utm_content=%v", user.UTMContent)
	}
	if user.FBCLID == nil || *user.FBCLID != "IwAR0abc_def-123" {
		t.Fatalf("fbclid=%v", user.FBCLID)
	}
	if user.TTCLID == nil || *user.TTCLID != "E.C.P.abc_DEF-123" {
		t.Fatalf("ttclid=%v", user.TTCLID)
	}

	exact := &domain.User{}
	(&RegisterAttribution{
		UTMSource: strings.Repeat("b", 100),
		UTMMedium: strings.Repeat("m", 150),
		FBCLID:    strings.Repeat("c", 256),
		TTCLID:    strings.Repeat("d", 256),
	}).Apply(exact)
	if exact.UTMSource == nil || *exact.UTMSource != strings.Repeat("b", 100) {
		t.Fatalf("100-char utm dropped: %v", exact.UTMSource)
	}
	if exact.UTMMedium == nil || *exact.UTMMedium != strings.Repeat("m", 100) {
		t.Fatalf("150-char utm_medium=%v", exact.UTMMedium)
	}
	if exact.FBCLID == nil || *exact.FBCLID != strings.Repeat("c", 256) {
		t.Fatalf("256-char fbclid=%v", exact.FBCLID)
	}
	if exact.TTCLID != nil {
		t.Fatalf("256-char ttclid stored: len=%d", len(*exact.TTCLID))
	}

	over := &domain.User{}
	(&RegisterAttribution{FBCLID: strings.Repeat("c", 257)}).Apply(over)
	if over.FBCLID != nil {
		t.Fatalf("257-char fbclid stored: len=%d", len(*over.FBCLID))
	}
}
