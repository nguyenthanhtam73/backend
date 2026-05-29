package ai

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/platform/openai"
)

const defaultTextCoachHTTPTimeout = 4 * time.Minute

// TextCoachProvider identifies which LLM produced a text-coaching completion.
type TextCoachProvider string

const (
	TextCoachProviderClaude TextCoachProvider = "claude"
	TextCoachProviderOpenAI TextCoachProvider = "openai"
)

// TextCoachResult is the raw assistant text plus metadata for logging / model_version.
type TextCoachResult struct {
	Text     string
	Provider TextCoachProvider
	Model    string
	Fallback bool // true when OpenAI was used after Claude failed
}

// TextCoachCompletion runs hybrid text coaching: Claude Sonnet first, GPT-4o on
// missing Anthropic key or Claude error. Vision pipelines should call this for
// the coach JSON pass — never for photo analysis.
func TextCoachCompletion(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	pipeline, system, user string,
) (TextCoachResult, error) {
	if cfg == nil {
		return TextCoachResult{}, fmt.Errorf("text coach: config required")
	}
	if strings.TrimSpace(system) == "" || strings.TrimSpace(user) == "" {
		return TextCoachResult{}, fmt.Errorf("text coach: system and user prompts required")
	}
	if httpClient == nil {
		httpClient = defaultTextCoachHTTPClient()
	}

	claudeKey := strings.TrimSpace(cfg.Anthropic.APIKey)
	openAIKey := strings.TrimSpace(cfg.OpenAI.APIKey)

	if claudeKey != "" {
		text, err := AnthropicMessages(ctx, cfg, httpClient, system, user)
		if err == nil && strings.TrimSpace(text) != "" {
			return TextCoachResult{
				Text:     text,
				Provider: TextCoachProviderClaude,
				Model:    cfg.AnthropicModel(),
			}, nil
		}
		if err != nil {
			slog.Warn("text coach: claude failed, trying openai fallback",
				"pipeline", pipeline,
				"model", cfg.AnthropicModel(),
				"err", err,
			)
		}
		if openAIKey == "" {
			if err != nil {
				return TextCoachResult{}, fmt.Errorf("text coach (%s): claude failed and openai key missing: %w", pipeline, err)
			}
			return TextCoachResult{}, fmt.Errorf("text coach (%s): claude returned empty and openai key missing", pipeline)
		}
		text, oErr := openai.ChatCompletionJSON(ctx, cfg, httpClient, system, user)
		if oErr != nil {
			if err != nil {
				return TextCoachResult{}, fmt.Errorf("text coach (%s): claude: %v; openai fallback: %w", pipeline, err, oErr)
			}
			return TextCoachResult{}, fmt.Errorf("text coach (%s): openai fallback: %w", pipeline, oErr)
		}
		return TextCoachResult{
			Text:     text,
			Provider: TextCoachProviderOpenAI,
			Model:    cfg.OpenAITextModel(),
			Fallback: true,
		}, nil
	}

	if openAIKey == "" {
		return TextCoachResult{}, fmt.Errorf("text coach (%s): set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY", pipeline)
	}

	text, err := openai.ChatCompletionJSON(ctx, cfg, httpClient, system, user)
	if err != nil {
		return TextCoachResult{}, fmt.Errorf("text coach (%s): openai: %w", pipeline, err)
	}
	return TextCoachResult{
		Text:     text,
		Provider: TextCoachProviderOpenAI,
		Model:    cfg.OpenAITextModel(),
	}, nil
}

func defaultTextCoachHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultTextCoachHTTPTimeout}
}
