package dto

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestLogFunnelEventRequest_StripsPathQueryAndFragment(t *testing.T) {
	const ts = "2026-09-29T10:43:00.123Z"
	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "plain", path: "/check-in", want: "/check-in"},
		{name: "query", path: "/check-in?email=a@b.com&token=secret", want: "/check-in"},
		{name: "fragment", path: "/check-in#section", want: "/check-in"},
		{name: "query then fragment", path: "/check-in?x=1#y", want: "/check-in"},
		{name: "fragment then query", path: "/check-in#y?email=a@b.com", want: "/check-in"},
		{name: "trim then strip", path: "  /check-in?token=abc  ", want: "/check-in"},
		{name: "unicode path", path: "/kiểm-tra?email=a@b.com", want: "/kiểm-tra"},
		{name: "only query", path: "?token=abc", want: ""},
		{name: "only fragment", path: "#top", want: ""},
		{name: "empty", path: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, msg := (LogFunnelEventRequest{
				Event:     domain.FunnelCheckinPageView,
				SessionID: "sess-1",
				Path:      tc.path,
				ClientTS:  ts,
			}).ValidateAndMap(uuid.Nil)
			if msg != "" {
				t.Fatalf("msg=%q", msg)
			}
			if row == nil || row.Path != tc.want {
				t.Fatalf("path=%q want %q", rowPath(row), tc.want)
			}
			if strings.ContainsAny(row.Path, "?#") || strings.Contains(row.Path, "@") {
				t.Fatalf("path still has query material: %q", row.Path)
			}
		})
	}
}

func TestLogFunnelEventRequest_PathLimitIgnoresQuery(t *testing.T) {
	const ts = "2026-09-29T10:43:00Z"
	okPath := "/" + strings.Repeat("p", domain.MaxFunnelPathRunes-1)
	row, msg := (LogFunnelEventRequest{
		Event:     domain.FunnelCheckinPageView,
		SessionID: "sess-1",
		Path:      okPath + "?token=" + strings.Repeat("s", 500),
		ClientTS:  ts,
	}).ValidateAndMap(uuid.Nil)
	if msg != "" {
		t.Fatalf("msg=%q", msg)
	}
	if row == nil || row.Path != okPath {
		t.Fatalf("path=%q want %d-rune path", rowPath(row), domain.MaxFunnelPathRunes)
	}

	tooLong := "/" + strings.Repeat("p", domain.MaxFunnelPathRunes)
	row, msg = (LogFunnelEventRequest{
		Event:     domain.FunnelCheckinPageView,
		SessionID: "sess-1",
		Path:      tooLong + "?x=1",
		ClientTS:  ts,
	}).ValidateAndMap(uuid.Nil)
	if row != nil || msg != "path is too long" {
		t.Fatalf("row=%v msg=%q", row, msg)
	}
}

func rowPath(row *domain.FunnelEvent) string {
	if row == nil {
		return ""
	}
	return row.Path
}
