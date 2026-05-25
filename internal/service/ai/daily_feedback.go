package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/platform/openai"
)

// GenerateDailyFeedback produces the same structured coach JSON as a skin-check coach turn,
// but without any photo / vision pass — only text context (profile snapshot, journal notes,
// goals). Use for reminders, weekly reviews, or text-only coaching endpoints.
//
// userContextMarkdown should ALREADY include the long-term memory block
// (BuildUserMemoryContext output) so the coach can personalise tone — callers
// are expected to concatenate USER_MEMORY + today's note before invoking.
//
// skillLevel should be "beginner", "intermediate", or "advanced" (empty defaults to intermediate).
func GenerateDailyFeedback(ctx context.Context, cfg *config.Config, userContextMarkdown string, skillLevel string) (*CoachStructuredOutput, error) {
	if cfg == nil {
		return nil, fmt.Errorf("ai daily feedback: config required")
	}
	u := strings.TrimSpace(userContextMarkdown)
	if u == "" {
		return nil, fmt.Errorf("ai daily feedback: user context required")
	}
	client := &http.Client{Timeout: 4 * time.Minute}

	userMsg := strings.Builder{}
	userMsg.WriteString("The user did not attach new photos for this turn. Base your coaching ONLY on USER_CONTEXT below (and acknowledge you have no fresh vision cues).\n\n")
	userMsg.WriteString("USER_CONTEXT:\n")
	userMsg.WriteString(u)
	userMsg.WriteString("\n\nNow produce the FINAL coach output as ONE JSON object matching this schema exactly.\n\n")
	userMsg.WriteString(CoachOutputJSONSchemaBlock)

	skill := strings.TrimSpace(skillLevel)
	if skill == "" {
		skill = "intermediate"
	}
	system := GetCoachPrompt(skill)
	textBody := userMsg.String()

	if strings.TrimSpace(cfg.Anthropic.APIKey) != "" {
		text, err := AnthropicMessages(ctx, cfg, client, system, textBody)
		if err != nil {
			return nil, fmt.Errorf("ai daily feedback (claude): %w", err)
		}
		raw, err := ExtractJSONObject(text)
		if err != nil {
			return nil, err
		}
		var out CoachStructuredOutput
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("ai daily feedback: parse json: %w", err)
		}
		return &out, nil
	}

	text, err := openai.ChatCompletionJSON(ctx, cfg, client, system, textBody)
	if err != nil {
		return nil, fmt.Errorf("ai daily feedback (openai): %w", err)
	}
	raw, err := ExtractJSONObject(text)
	if err != nil {
		return nil, err
	}
	var out CoachStructuredOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("ai daily feedback: parse json: %w", err)
	}
	return &out, nil
}
