package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
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
		"chưa có thông tin đầy đủ",
		"không có thông tin",
		"chưa đủ thông tin",
		"body butter",
		"fine for body use",
		"dầu dừa",
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
		"da dầu",
		"vùng chữ T còn bóng",
		"skin type is known",
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

func TestWardrobeInsightChatBodyIsDeterministic(t *testing.T) {
	cfg := &config.Config{OpenAI: config.OpenAIConfig{Model: "gpt-4o"}}
	body := wardrobeProductInsightChatBody(cfg, "user text", "")
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Model          string  `json:"model"`
		Temperature    float64 `json:"temperature"`
		TopP           float64 `json:"top_p"`
		Seed           int     `json:"seed"`
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
		Messages []map[string]string `json:"messages"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "gpt-4o" || got.Temperature != 0 || got.TopP != 1 || got.Seed != wardrobeInsightSeed {
		t.Fatalf("sampling: model %q temp %v top_p %v seed %d", got.Model, got.Temperature, got.TopP, got.Seed)
	}
	if got.ResponseFormat.Type != "json_object" {
		t.Fatalf("response_format: %+v", got.ResponseFormat)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages: %d", len(got.Messages))
	}
	corrected := wardrobeProductInsightChatBody(cfg, "user text", "fix the card")
	raw, err = json.Marshal(corrected)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 3 || got.Messages[2]["content"] != "fix the card" {
		t.Fatalf("correction message: %+v", got.Messages)
	}
}
