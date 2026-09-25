package ai

import (
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
)

func TestWardrobeProductInsightPromptLocksCard(t *testing.T) {
	p := WardrobeProductInsightSystemPrompt()
	for _, s := range []string{
		"what_it_does",
		"yes",
		"maybe",
		"no",
		"nên mua",
		"chưa nên",
		"Nên dùng tiếp",
		"Chưa nên dùng tiếp",
		"ALREADY OWNS",
		"skin type, concerns, and goal",
		"The word \"mua\" is forbidden",
		"Nên dùng tiếp vì hợp với da dầu và mục tiêu làm sạch mụn.",
		"actives",
		"gloss",
	} {
		if !strings.Contains(p, s) {
			t.Fatalf("prompt missing %q", s)
		}
	}
	if strings.Contains(strings.ToLower(p), "freeform") {
		t.Fatal("prompt should not invite a chat reply")
	}
	if strings.Contains(p, "not to add another") {
		t.Fatal("owned-product card must not use the old shopping rule about adding another product")
	}
	// Machine tokens stay so the cabinet UI can map them. The sentence under
	// the label is keep-using copy, not a buy reason.
	if !strings.Contains(p, `Write exactly "nên mua" or "chưa nên"`) {
		t.Fatal("prompt must keep the buy.advice tokens the cabinet UI maps")
	}
}

func TestBuildWardrobeProductInsightUser_UsesProfileAndCheckIns(t *testing.T) {
	text, known := buildWardrobeProductInsightUser(WardrobeProductInsightRequest{
		Name:     "Sữa rửa mặt dịu",
		Brand:    "CeraVe",
		Category: "cleanser",
		Notes:    "dùng tối",
		Profile:  &domain.SkinProfile{SkinType: "oily"},
		Recent: []domain.SkinCheck{{
			CheckDate: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
			UserNote:  "vùng chữ T còn bóng",
		}},
	})
	if !known {
		t.Fatal("expected skin context")
	}
	for _, s := range []string{
		"already in the user's cabinet",
		"nên dùng tiếp",
		"chưa nên dùng tiếp",
		"Do not recommend buying",
		"Sữa rửa mặt dịu",
		"CeraVe",
		"cleanser",
		"oily",
		"vùng chữ T còn bóng",
	} {
		if !strings.Contains(text, s) {
			t.Fatalf("user text missing %q\n%s", s, text)
		}
	}
}

func TestWardrobeInsightSkinKnown(t *testing.T) {
	if wardrobeInsightSkinKnown(BuildSkinProfileContext(nil), "") {
		t.Fatal("no profile")
	}
	if wardrobeInsightSkinKnown(BuildSkinProfileContext(&domain.SkinProfile{}), "") {
		t.Fatal("sparse profile")
	}
	if !wardrobeInsightSkinKnown(BuildSkinProfileContext(&domain.SkinProfile{SkinType: "dry"}), "") {
		t.Fatal("skin type")
	}
	recent := BuildRecentCheckInsContext([]domain.SkinCheck{{
		CheckDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		UserNote:  "da đang rát",
	}})
	if !wardrobeInsightSkinKnown(BuildSkinProfileContext(nil), recent) {
		t.Fatal("recent check-in")
	}
	_, known := buildWardrobeProductInsightUser(WardrobeProductInsightRequest{Name: "Kem"})
	if known {
		t.Fatal("name alone is not skin context")
	}
}
