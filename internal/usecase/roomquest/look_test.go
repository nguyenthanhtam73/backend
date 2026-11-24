package roomquest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
)

func TestLookJSONBubble(t *testing.T) {
	worn := lookJSON(domain.MascotLoadout{Body: "body_chibi", Bubble: "mint_mat"}, "")
	if worn.Bubble != "mint_mat" {
		t.Fatalf("bubble %s", worn.Bubble)
	}
	raw, err := json.Marshal(worn)
	if err != nil || !strings.Contains(string(raw), `"bubble":"mint_mat"`) {
		t.Fatalf("today look %s %v", raw, err)
	}
	plain := lookJSON(domain.MascotLoadout{Body: "body_chibi"}, "")
	raw, err = json.Marshal(plain)
	if err != nil || strings.Contains(string(raw), "bubble") {
		t.Fatalf("default look %s %v", raw, err)
	}
}
