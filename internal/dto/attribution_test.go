package dto

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
)

func TestAttributionPatterns_DropInvalidKeepValid(t *testing.T) {
	user := &domain.User{}
	attr := &RegisterAttribution{
		UTMSource:   "person@example.com",
		UTMMedium:   "paid social",
		UTMCampaign: "summer%20sale",
		UTMContent:  strings.Repeat("a", 101),
		FBCLID:      "IwAR0abc_def-123",
		TTCLID:      "E.C.P.abc_DEF-123",
	}
	attr.Apply(user)

	if user.UTMSource != nil {
		t.Fatalf("email-looking utm_source stored: %s", *user.UTMSource)
	}
	if user.UTMMedium != nil {
		t.Fatalf("spaced utm_medium stored: %s", *user.UTMMedium)
	}
	if user.UTMCampaign != nil {
		t.Fatalf("percent utm_campaign stored: %s", *user.UTMCampaign)
	}
	if user.UTMContent != nil {
		t.Fatalf("101-char utm_content stored: %s", *user.UTMContent)
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
		FBCLID:    strings.Repeat("c", 255),
		TTCLID:    strings.Repeat("d", 256),
	}).Apply(exact)
	if exact.UTMSource == nil || *exact.UTMSource != strings.Repeat("b", 100) {
		t.Fatalf("100-char utm dropped: %v", exact.UTMSource)
	}
	if exact.FBCLID == nil || *exact.FBCLID != strings.Repeat("c", 255) {
		t.Fatalf("255-char fbclid dropped")
	}
	if exact.TTCLID != nil {
		t.Fatalf("256-char ttclid stored: len=%d", len(*exact.TTCLID))
	}
}
