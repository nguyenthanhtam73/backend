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

	var claudeErr error
	if claudeKey != "" {
		// Guard Claude with its own circuit breaker: after a sustained Claude
		// outage the breaker trips Open and this call returns instantly
		// (ErrCircuitOpen) instead of hammering a struggling provider — we then
		// fall through to the OpenAI fallback below. A fresh key with a healthy
		// provider is unaffected (breaker stays Closed).
		text, err := CallAIWithCircuitBreaker(ctx, "claude", func(ctx context.Context) (string, error) {
			return AnthropicMessages(ctx, cfg, httpClient, system, user)
		})
		if err == nil && strings.TrimSpace(text) != "" {
			res := TextCoachResult{
				Text:     text,
				Provider: TextCoachProviderClaude,
				Model:    cfg.AnthropicModel(),
			}
			logTextCoachSelection(pipeline, res, nil)
			return res, nil
		}
		claudeErr = err
		if claudeErr != nil {
			slog.Warn("text coach: claude unavailable, will try openai fallback",
				"pipeline", pipeline,
				"model", cfg.AnthropicModel(),
				"err", claudeErr,
			)
		} else {
			claudeErr = fmt.Errorf("empty response")
			slog.Warn("text coach: claude returned empty, will try openai fallback",
				"pipeline", pipeline,
				"model", cfg.AnthropicModel(),
			)
		}
		if openAIKey == "" {
			return TextCoachResult{}, fmt.Errorf("text coach (%s): claude failed and openai key missing: %w", pipeline, claudeErr)
		}
		text, oErr := CallAIWithCircuitBreaker(ctx, "openai", func(ctx context.Context) (string, error) {
			return openai.ChatCompletionJSON(ctx, cfg, httpClient, system, user)
		})
		if oErr != nil {
			return TextCoachResult{}, fmt.Errorf("text coach (%s): claude: %v; openai fallback: %w", pipeline, claudeErr, oErr)
		}
		res := TextCoachResult{
			Text:     text,
			Provider: TextCoachProviderOpenAI,
			Model:    cfg.OpenAITextModel(),
			Fallback: true,
		}
		logTextCoachSelection(pipeline, res, claudeErr)
		return res, nil
	}

	if openAIKey == "" {
		return TextCoachResult{}, fmt.Errorf("text coach (%s): set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY", pipeline)
	}

	text, err := CallAIWithCircuitBreaker(ctx, "openai", func(ctx context.Context) (string, error) {
		return openai.ChatCompletionJSON(ctx, cfg, httpClient, system, user)
	})
	if err != nil {
		return TextCoachResult{}, fmt.Errorf("text coach (%s): openai: %w", pipeline, err)
	}
	res := TextCoachResult{
		Text:     text,
		Provider: TextCoachProviderOpenAI,
		Model:    cfg.OpenAITextModel(),
	}
	logTextCoachSelection(pipeline, res, nil)
	return res, nil
}

func defaultTextCoachHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultTextCoachHTTPTimeout}
}
