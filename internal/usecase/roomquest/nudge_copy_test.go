package roomquest

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNudgeMessageIsFixedAndNamed(t *testing.T) {
	sender := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	target := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	a := NudgeMessage("Lan\nNguyễn", "", sender, target, "2026-11-19")
	b := NudgeMessage("Lan Nguyễn", "", sender, target, "2026-11-19")
	if a != b {
		t.Fatalf("unstable %q %q", a, b)
	}
	if !strings.Contains(a, "Lan Nguyễn") || strings.Contains(a, "\n") || strings.Contains(a, "{name}") {
		t.Fatalf("message %q", a)
	}
	blank := NudgeMessage("  ", "", sender, target, "2026-11-19")
	if !strings.Contains(blank, "Một bạn") {
		t.Fatalf("fallback %q", blank)
	}
	for _, tmpl := range nudgeTemplates {
		if strings.Contains(a, strings.TrimPrefix(strings.ReplaceAll(tmpl, "{name}", ""), " ")) {
			return
		}
	}
	t.Fatalf("not from the list: %q", a)
}
