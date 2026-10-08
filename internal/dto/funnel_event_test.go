package dto

import (
	"encoding/json"
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

func TestLogFunnelEventRequest_RegisterCampaignSanitizedOnce(t *testing.T) {
	// "C%2B%2B" decodes to "C++" in one pass. '+' is allowed, so the stored
	// campaign is "C++". A second pass would turn those pluses into spaces
	// and trim the value down to "C".
	row, msg := (LogFunnelEventRequest{
		Event:     domain.FunnelRegisterFormView,
		SessionID: "sess-cpp",
		Path:      "/register",
		Props:     json.RawMessage(`{"utm_campaign":"C%2B%2B"}`),
		ClientTS:  "2026-09-29T10:43:00Z",
	}).ValidateAndMap(uuid.Nil)
	if msg != "" || row == nil {
		t.Fatalf("msg=%q row=%v", msg, row)
	}
	if row.UTMCampaign == nil || *row.UTMCampaign != "C++" {
		t.Fatalf("utm_campaign=%v, want C++", row.UTMCampaign)
	}
	var props map[string]string
	if err := json.Unmarshal(row.Props, &props); err != nil {
		t.Fatal(err)
	}
	if props["utm_campaign"] != "C++" {
		t.Fatalf("props=%s", row.Props)
	}
}

func TestLogFunnelEventRequest_DropsUnknownRegisterAndLandingKeys(t *testing.T) {
	reg, msg := (LogFunnelEventRequest{
		Event:     domain.FunnelRegisterFormView,
		SessionID: "sess-reg",
		Path:      "/register",
		Props:     json.RawMessage(`{"utm_source":"meta","utm_medium":"cpc","ttclid":"abc"}`),
		ClientTS:  "2026-09-29T10:43:00Z",
	}).ValidateAndMap(uuid.Nil)
	if msg != "" || reg == nil {
		t.Fatalf("register msg=%q row=%v", msg, reg)
	}
	if string(reg.Props) != `{"utm_source":"meta"}` && !propsEqual(reg.Props, map[string]any{"utm_source": "meta"}) {
		t.Fatalf("register props=%s", reg.Props)
	}

	cta, msg := (LogFunnelEventRequest{
		Event:     domain.FunnelLandingCTAClick,
		SessionID: "sess-cta",
		Path:      "/",
		Props:     json.RawMessage(`{"button":"hero_primary","utm_medium":"cpc","label":"Sign up"}`),
		ClientTS:  "2026-09-29T10:43:00Z",
	}).ValidateAndMap(uuid.Nil)
	if msg != "" || cta == nil {
		t.Fatalf("landing msg=%q row=%v", msg, cta)
	}
	if !propsEqual(cta.Props, map[string]any{"button": "hero_primary"}) {
		t.Fatalf("landing props=%s", cta.Props)
	}

	_, msg = (LogFunnelEventRequest{
		Event:     domain.FunnelLandingCTAClick,
		SessionID: "sess-bad",
		Path:      "/",
		Props:     json.RawMessage(`{"button":"nope","utm_medium":"cpc"}`),
		ClientTS:  "2026-09-29T10:43:00Z",
	}).ValidateAndMap(uuid.Nil)
	if msg != "invalid button" {
		t.Fatalf("invalid button msg=%q", msg)
	}
}

func propsEqual(raw json.RawMessage, want map[string]any) bool {
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil || len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

func rowPath(row *domain.FunnelEvent) string {
	if row == nil {
		return ""
	}
	return row.Path
}
