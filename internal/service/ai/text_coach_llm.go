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

// Per-provider soft deadlines for the coach text pass. These bound how long we
// wait on a slow (not errored) provider before giving up and failing over, so
// one sluggish model can't burn the whole analysis budget. They stay attached
// to the provider, not to its position in TextCoachProviderOrder, and they
// derive from the caller's ctx, so the parent job/HTTP timeouts still apply
// as hard ceilings.
//
// Claude is capped at 60s (was 75s): with brevity constraints + the Haiku default
// a healthy coach generation now finishes in ~10–25s (Sonnet was ~20–35s), so 60s
// still comfortably covers a slow-but-working call. OpenAI keeps its own 45s
// budget, so a slow primary does not starve the fallback.
const (
	claudeCoachTimeout = 60 * time.Second
	openaiCoachTimeout = 45 * time.Second
)

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
	Fallback bool // true when the second provider answered after the first one failed
}

// textCoachCall is one configured provider in TextCoachProviderOrder.
// The slice is built from that order; this struct does not rank providers.
type textCoachCall struct {
	provider TextCoachProvider
	label    string
	model    string
	timeout  time.Duration
	breaker  string
	fn       func(context.Context) (string, error)
}

// TextCoachCompletion runs hybrid text coaching in TextCoachProviderOrder.
// Default (AI_PRIMARY_PROVIDER unset, openai, or unknown): GPT-4o first, Claude
// on a missing OpenAI key or an OpenAI error. AI_PRIMARY_PROVIDER=anthropic
// keeps the previous order: Claude first (Haiku when DADIARY_ANTHROPIC_FAST_MODEL
// is set, otherwise Sonnet), GPT-4o on a missing Anthropic key or a Claude error.
// Per-provider timeouts are unchanged (Claude 60s, OpenAI 45s).
//
// Every text-coach call site uses this function — skin check-in, onboarding
// skin, starter routine, daily feedback, routine suggest, share titles — so
// none of them pick an order. Vision pipelines call this for the coach JSON
// pass only, never for photo analysis.
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

	order := TextCoachProviderOrder(cfg)
	calls := make([]textCoachCall, 0, len(order))
	for _, provider := range order {
		call, ok := newTextCoachCall(cfg, httpClient, provider, system, user)
		if ok {
			calls = append(calls, call)
		}
	}
	if len(calls) == 0 {
		return TextCoachResult{}, fmt.Errorf("text coach (%s): set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY", pipeline)
	}

	var firstErr, lastErr error
	for i, call := range calls {
		callCtx, cancel := context.WithTimeout(ctx, call.timeout)
		start := time.Now()
		text, err := CallAIWithCircuitBreaker(callCtx, call.breaker, call.fn)
		elapsed := time.Since(start)
		cancel()
		if err == nil && strings.TrimSpace(text) != "" {
			res := TextCoachResult{
				Text:     text,
				Provider: call.provider,
				Model:    call.model,
				// Fallback only after a real attempt failed. A missing primary
				// key skips that provider, so the other one is not a fallback.
				Fallback: i > 0,
			}
			logTextCoachSelection(pipeline, res, firstErr, elapsed, i == 0 && call.provider != order[0])
			return res, nil
		}
		empty := err == nil
		if empty {
			err = fmt.Errorf("empty response")
		}
		if firstErr == nil {
			firstErr = err
		}
		lastErr = err
		if i == len(calls)-1 {
			break
		}
		next := calls[i+1].label
		if empty {
			slog.Warn(fmt.Sprintf("text coach: %s returned empty, will try %s fallback", call.label, next),
				"pipeline", pipeline,
				"model", call.model,
			)
			continue
		}
		slog.Warn(fmt.Sprintf("text coach: %s unavailable, will try %s fallback", call.label, next),
			"pipeline", pipeline,
			"model", call.model,
			"err", err,
		)
	}

	// One configured provider: either the primary failed and the other key is
	// missing, or the primary key was missing and the remaining provider failed.
	if len(calls) == 1 {
		only := calls[0]
		if only.provider != order[0] {
			return TextCoachResult{}, fmt.Errorf("text coach (%s): %s: %w", pipeline, only.label, lastErr)
		}
		return TextCoachResult{}, fmt.Errorf("text coach (%s): %s failed and %s key missing: %w", pipeline, only.label, providerLabel(order[1]), lastErr)
	}
	return TextCoachResult{}, fmt.Errorf("text coach (%s): %s: %v; %s fallback: %w", pipeline, calls[0].label, firstErr, calls[len(calls)-1].label, lastErr)
}

func newTextCoachCall(
	cfg *config.Config,
	httpClient *http.Client,
	provider TextCoachProvider,
	system, user string,
) (textCoachCall, bool) {
	switch provider {
	case TextCoachProviderClaude:
		if strings.TrimSpace(cfg.Anthropic.APIKey) == "" {
			return textCoachCall{}, false
		}
		return textCoachCall{
			provider: provider,
			label:    "claude",
			model:    cfg.AnthropicCoachModel(),
			timeout:  claudeCoachTimeout,
			breaker:  "claude",
			fn: func(ctx context.Context) (string, error) {
				return AnthropicMessages(ctx, cfg, httpClient, system, user)
			},
		}, true
	case TextCoachProviderOpenAI:
		if strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
			return textCoachCall{}, false
		}
		return textCoachCall{
			provider: provider,
			label:    "openai",
			model:    cfg.OpenAITextModel(),
			timeout:  openaiCoachTimeout,
			breaker:  "openai",
			fn: func(ctx context.Context) (string, error) {
				return openai.ChatCompletionJSON(ctx, cfg, httpClient, system, user)
			},
		}, true
	default:
		return textCoachCall{}, false
	}
}

func providerLabel(provider TextCoachProvider) string {
	switch provider {
	case TextCoachProviderClaude:
		return "claude"
	case TextCoachProviderOpenAI:
		return "openai"
	default:
		return string(provider)
	}
}

func defaultTextCoachHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultTextCoachHTTPTimeout}
}
