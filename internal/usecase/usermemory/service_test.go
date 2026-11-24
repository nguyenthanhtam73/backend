package usermemory

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/google/uuid"
)

func TestGet_FreshUserEmptySummary(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil)
	res, err := svc.Get(context.Background(), uuid.New(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Summary) != 0 {
		t.Fatalf("summary %#v", res.Summary)
	}
	if res.MemoryText != "" {
		t.Fatalf("prompt leaked %q", res.MemoryText)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	assertNoPromptMarkers(t, body)
	if !strings.Contains(body, `"memory_text":""`) {
		t.Fatalf("memory_text %s", body)
	}
	if !strings.Contains(body, `"summary":[]`) {
		t.Fatalf("summary %s", body)
	}
	if !strings.Contains(body, `"sections_present":[]`) {
		t.Fatalf("sections_present %s", body)
	}
}

func TestAssemble_SavedMemoryOmitsPrompt(t *testing.T) {
	prompt := ai.BuildUserMemoryContext(context.Background(), uuid.New(), ai.UserMemoryDeps{}, ai.UserMemoryOptions{})
	prompt += "\n## Saved SkinProfile\nskin type noted\n## TODAY context\nGUIDANCE: paraphrase warmly, không quote nguyên văn.\n"
	if !strings.Contains(prompt, "USER_MEMORY") || !strings.Contains(prompt, "no saved memory") {
		t.Fatalf("llm prompt changed: %q", prompt)
	}
	res := assembleUserMemoryResponse(uuid.New(), prompt, false, []string{"Ngày 22/10 · 1 ảnh"}, dto.UserMemoryStats{
		SectionsPresent: []string{"profile", "recent_checks"},
		AdherenceTier:   "strong",
	})
	if len(res.Summary) != 1 || res.Summary[0] != "Ngày 22/10 · 1 ảnh" {
		t.Fatalf("summary %#v", res.Summary)
	}
	if res.MemoryText != "" {
		t.Fatalf("prompt copied %q", res.MemoryText)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "Ngày 22/10 · 1 ảnh") {
		t.Fatalf("summary missing: %s", body)
	}
	if !strings.Contains(body, `"memory_text":""`) {
		t.Fatalf("memory_text %s", body)
	}
	assertNoPromptMarkers(t, body)
}

func assertNoPromptMarkers(t *testing.T, body string) {
	t.Helper()
	for _, bad := range []string{
		"USER_MEMORY",
		"TODAY",
		"paraphrase",
		"no saved memory",
		"GUIDANCE",
	} {
		if strings.Contains(body, bad) {
			t.Fatalf("response contains %q: %s", bad, body)
		}
	}
	if re := regexp.MustCompile(`\b[A-Z]{2,}(?:_[A-Z]+)*\b`); re.MatchString(body) {
		t.Fatalf("all-caps label in %s", body)
	}
}
