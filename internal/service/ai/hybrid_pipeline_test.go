package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/platform/openai"
	"github.com/google/uuid"
)

const (
	mockCoachClaudeMarker = "from-claude-coach"
	mockCoachGPTMarker    = "from-gpt-coach"
	mockRoutineClaude     = "from-claude-routine"
	mockRoutineGPT        = "from-gpt-routine"
	mockStarterClaude     = "from-claude-starter"
	mockStarterGPT        = "from-gpt-starter"
)

func mockCoachJSON(marker string) string {
	return `{
  "score": 0.75,
  "strengths": ["Bạn tick routine đều — giỏi lắm"],
  "situation_analysis": "` + marker + ` mấy lần gần đây da ổn",
  "improvements": [{"tip":"tip","why":"why"}],
  "routine_hints": ["Sáng: test hint"],
  "avoid_or_patch": [],
  "safety_reminders": ["patch test"],
  "skin_scores": {"hydration":0.5,"clarity":0.5,"barrier":0.5},
  "concern_alignment": "aligned",
  "medical_disclaimer": "not medical advice",
  "summary_notes": "Tiếp tục duy trì routine nhé"
}`
}

func mockRoutineJSON(marker string) string {
	return `{
  "morning": ["Sáng: rửa mặt"],
  "evening": ["Tối: dưỡng ẩm"],
  "encouragement": "` + marker + `",
  "rationale": "rationale",
  "week_notes": "",
  "safety_notes": "patch test",
  "closing_reminder": "ok"
}`
}

func mockStarterJSON(marker string) string {
	return `{
  "encouragement": "` + marker + `",
  "skin_readback": "oily skin",
  "morning": ["Sáng: rửa mặt"],
  "evening": ["Tối: dưỡng ẩm"],
  "rationale": "rationale",
  "week_notes": "",
  "safety_notes": "patch test",
  "closing_reminder": "ok"
}`
}

type hybridMockMode int

const (
	hybridClaudeOK hybridMockMode = iota
	hybridClaudeFail
)

func newPipelineMockServer(t *testing.T, mode hybridMockMode, claudeBody, gptBody string) (*httptest.Server, *[]string) {
	t.Helper()
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/messages":
			if mode == hybridClaudeFail {
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte(`{"error":"timeout"}`))
				return
			}
			_, _ = w.Write([]byte(`{"content":[{"type":"text","text":` + jsonQuote(claudeBody) + `}]}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + jsonQuote(gptBody) + `}}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	return srv, &hits
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func wireHybridEndpoints(t *testing.T, srv *httptest.Server) func() {
	t.Helper()
	prevAnthropic := anthropicMessagesURL
	anthropicMessagesURL = srv.URL + "/v1/messages"
	restoreOpenAI := openai.SetChatCompletionsURLForTest(srv.URL + "/v1/chat/completions")
	return func() {
		anthropicMessagesURL = prevAnthropic
		restoreOpenAI()
	}
}

func hybridTestConfig() *config.Config {
	return &config.Config{
		Anthropic: config.AnthropicConfig{APIKey: "sk-ant-test", Model: "claude-sonnet-4-20250514"},
		OpenAI:    config.OpenAIConfig{APIKey: "sk-test", Model: "gpt-4o", VisionModel: "gpt-4o-mini"},
	}
}

func TestHybridPipeline_DailyFeedback(t *testing.T) {
	persona := personaBeginnerOily()
	ctx := context.Background()

	t.Run("claude_success", func(t *testing.T) {
		srv, hits := newPipelineMockServer(t, hybridClaudeOK, mockCoachJSON(mockCoachClaudeMarker), mockCoachJSON(mockCoachGPTMarker))
		defer srv.Close()
		cleanup := wireHybridEndpoints(t, srv)
		defer cleanup()

		out, err := GenerateDailyFeedback(ctx, hybridTestConfig(), persona.FullContextWithMemory(), persona.SkillLevel)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.SituationAnalysis, mockCoachClaudeMarker) {
			t.Fatalf("want claude marker, got %q", out.SituationAnalysis)
		}
		if len(*hits) != 1 || (*hits)[0] != "/v1/messages" {
			t.Fatalf("calls=%v want claude only", *hits)
		}
	})

	t.Run("claude_fallback", func(t *testing.T) {
		srv, hits := newPipelineMockServer(t, hybridClaudeFail, mockCoachJSON(mockCoachClaudeMarker), mockCoachJSON(mockCoachGPTMarker))
		defer srv.Close()
		cleanup := wireHybridEndpoints(t, srv)
		defer cleanup()

		out, err := GenerateDailyFeedback(ctx, hybridTestConfig(), persona.FullContextWithMemory(), persona.SkillLevel)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.SituationAnalysis, mockCoachGPTMarker) {
			t.Fatalf("want gpt marker, got %q", out.SituationAnalysis)
		}
		if len(*hits) != 2 || (*hits)[0] != "/v1/messages" || (*hits)[1] != "/v1/chat/completions" {
			t.Fatalf("calls=%v want claude then gpt", *hits)
		}
	})
}

func TestHybridPipeline_RoutineSuggest(t *testing.T) {
	persona := personaIntermediateCombo()
	ctx := context.Background()
	in := SuggestRoutineInput{
		Profile:    persona.Profile,
		LastCheck:  persona.TodayCheck,
		Locale:     "vi",
		SkillMode:  persona.SkillLevel,
		UserMemory: persona.Memory,
	}

	t.Run("claude_success", func(t *testing.T) {
		srv, hits := newPipelineMockServer(t, hybridClaudeOK, mockRoutineJSON(mockRoutineClaude), mockRoutineJSON(mockRoutineGPT))
		defer srv.Close()
		cleanup := wireHybridEndpoints(t, srv)
		defer cleanup()

		out, err := GenerateSuggestedRoutine(ctx, hybridTestConfig(), in)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.Encouragement, mockRoutineClaude) {
			t.Fatalf("want claude marker, got %q", out.Encouragement)
		}
		if len(*hits) != 1 {
			t.Fatalf("calls=%v", *hits)
		}
	})

	t.Run("claude_fallback", func(t *testing.T) {
		srv, hits := newPipelineMockServer(t, hybridClaudeFail, mockRoutineJSON(mockRoutineClaude), mockRoutineJSON(mockRoutineGPT))
		defer srv.Close()
		cleanup := wireHybridEndpoints(t, srv)
		defer cleanup()

		out, err := GenerateSuggestedRoutine(ctx, hybridTestConfig(), in)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.Encouragement, mockRoutineGPT) {
			t.Fatalf("want gpt marker, got %q", out.Encouragement)
		}
		if len(*hits) != 2 {
			t.Fatalf("calls=%v", *hits)
		}
	})
}

func TestHybridPipeline_StarterRoutine(t *testing.T) {
	ctx := context.Background()
	onboarding, _ := json.Marshal(map[string]string{"skin_type": "oily", "goal": "clearer_skin"})

	t.Run("claude_success", func(t *testing.T) {
		srv, hits := newPipelineMockServer(t, hybridClaudeOK, mockStarterJSON(mockStarterClaude), mockStarterJSON(mockStarterGPT))
		defer srv.Close()
		cleanup := wireHybridEndpoints(t, srv)
		defer cleanup()

		out, err := GenerateStarterRoutine(ctx, hybridTestConfig(), onboarding, "vi", "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.Encouragement, mockStarterClaude) {
			t.Fatalf("want claude marker, got %q", out.Encouragement)
		}
		if len(*hits) != 1 {
			t.Fatalf("calls=%v", *hits)
		}
	})

	t.Run("claude_fallback", func(t *testing.T) {
		srv, hits := newPipelineMockServer(t, hybridClaudeFail, mockStarterJSON(mockStarterClaude), mockStarterJSON(mockStarterGPT))
		defer srv.Close()
		cleanup := wireHybridEndpoints(t, srv)
		defer cleanup()

		out, err := GenerateStarterRoutine(ctx, hybridTestConfig(), onboarding, "vi", "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.Encouragement, mockStarterGPT) {
			t.Fatalf("want gpt marker, got %q", out.Encouragement)
		}
		if len(*hits) != 2 {
			t.Fatalf("calls=%v", *hits)
		}
	})
}

func TestHybridPipeline_OpenAIOnlyDoesNotCrash(t *testing.T) {
	srv, _ := newPipelineMockServer(t, hybridClaudeOK, mockCoachJSON(mockCoachClaudeMarker), mockCoachJSON(mockCoachGPTMarker))
	defer srv.Close()
	cleanup := wireHybridEndpoints(t, srv)
	defer cleanup()

	cfg := hybridTestConfig()
	cfg.Anthropic.APIKey = ""

	check := &domain.SkinCheck{
		ID:       uuid.New(),
		UserID:   uuid.New(),
		UserNote: "test",
	}
	profile := &domain.SkinProfile{SkinType: "oily", SkillLevel: domain.SkillLevelBeginner}

	ctx := context.Background()
	_, _, err := RunSkinCheckCoach(ctx, cfg, srv.Client(), t.TempDir(), check, profile, "")
	if err == nil {
		// expected: no images — but should not panic; vision skipped gracefully
	}
	if err != nil && !strings.Contains(err.Error(), "no image") && !strings.Contains(err.Error(), "openai") {
		t.Fatalf("unexpected error: %v", err)
	}
}
