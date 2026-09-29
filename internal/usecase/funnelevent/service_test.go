package funnelevent

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
)

func TestTrimUserAgent(t *testing.T) {
	if got := trimUserAgent("  Mozilla/5.0\nBot\t "); got != "Mozilla/5.0Bot" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("á", domain.MaxFunnelUserAgentRunes+40)
	got := trimUserAgent(long)
	if utf8.RuneCountInString(got) != domain.MaxFunnelUserAgentRunes {
		t.Fatalf("runes=%d", utf8.RuneCountInString(got))
	}
	if got != strings.Repeat("á", domain.MaxFunnelUserAgentRunes) {
		t.Fatal("truncated user agent was not a rune-safe prefix")
	}
}
