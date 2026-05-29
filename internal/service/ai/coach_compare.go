package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
)

const coachCompareHTTPTimeout = 90 * time.Second

// LLMUsage holds token counts from provider API responses.
type LLMUsage struct {
	InputTokens  int
	OutputTokens int
}

func (u LLMUsage) Total() int {
	return u.InputTokens + u.OutputTokens
}

// CompareLLMResult is one forced-provider coach LLM call (for A/B live tests).
type CompareLLMResult struct {
	Provider string
	Model    string
	Latency  time.Duration
	Usage    LLMUsage
	Text     string
	Err      error
}

// CoachQualityReport scores coach output on DaDiary QA dimensions.
type CoachQualityReport struct {
	Persona            CoachPersonalizationResult
	SupportiveTone     bool
	Encouragement      bool
	HasSafety          bool
	SpecificityScore   float64
	CompositeScore     float64
}

// RoutineQualityReport scores routine suggest output.
type RoutineQualityReport struct {
	Persona          CoachPersonalizationResult
	SupportiveTone   bool
	Encouragement    bool
	HasSafety        bool
	CompositeScore   float64
}

// cloneConfigForProvider returns a copy that forces a single text-coach provider.
func cloneConfigForProvider(base *config.Config, provider TextCoachProvider) *config.Config {
	if base == nil {
		return nil
	}
	c := *base
	switch provider {
	case TextCoachProviderClaude:
		c.OpenAI = config.OpenAIConfig{APIKey: ""}
	case TextCoachProviderOpenAI:
		c.Anthropic = config.AnthropicConfig{APIKey: ""}
	}
	return &c
}

// callCompareLLM invokes Claude or OpenAI directly (no hybrid fallback).
func callCompareLLM(
	ctx context.Context,
	cfg *config.Config,
	provider TextCoachProvider,
	system, user string,
) CompareLLMResult {
	start := time.Now()
	client := &http.Client{Timeout: coachCompareHTTPTimeout}
	res := CompareLLMResult{Provider: string(provider)}

	switch provider {
	case TextCoachProviderClaude:
		res.Model = cfg.AnthropicModel()
		text, usage, err := anthropicMessagesUsage(ctx, cfg, client, system, user)
		res.Text, res.Usage, res.Err = text, usage, err
	case TextCoachProviderOpenAI:
		res.Model = cfg.OpenAITextModel()
		text, usage, err := openAIChatUsage(ctx, cfg, client, system, user)
		res.Text, res.Usage, res.Err = text, usage, err
	default:
		res.Err = fmt.Errorf("unknown provider %q", provider)
	}
	res.Latency = time.Since(start)
	return res
}

func anthropicMessagesUsage(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	system, user string,
) (string, LLMUsage, error) {
	if cfg == nil || strings.TrimSpace(cfg.Anthropic.APIKey) == "" {
		return "", LLMUsage{}, fmt.Errorf("anthropic: missing api key")
	}
	model := cfg.AnthropicModel()
	body := map[string]any{
		"model":       model,
		"max_tokens":  8192,
		"temperature": 0.25,
		"system":      system,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": user},
				},
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", LLMUsage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicMessagesURL, bytes.NewReader(payload))
	if err != nil {
		return "", LLMUsage{}, err
	}
	req.Header.Set("x-api-key", cfg.Anthropic.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", LLMUsage{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", LLMUsage{}, fmt.Errorf("anthropic messages http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return "", LLMUsage{}, err
	}
	var out strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" && block.Text != "" {
			out.WriteString(block.Text)
		}
	}
	s := strings.TrimSpace(out.String())
	if s == "" {
		return "", LLMUsage{}, fmt.Errorf("anthropic: empty assistant text")
	}
	return s, LLMUsage{InputTokens: parsed.Usage.InputTokens, OutputTokens: parsed.Usage.OutputTokens}, nil
}

func openAIChatUsage(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	system, user string,
) (string, LLMUsage, error) {
	if cfg == nil || strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return "", LLMUsage{}, fmt.Errorf("openai: missing api key")
	}
	model := cfg.OpenAITextModel()
	body := map[string]any{
		"model":           model,
		"temperature":     0.4,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]any{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", LLMUsage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIChatCompletionsURL(), bytes.NewReader(payload))
	if err != nil {
		return "", LLMUsage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAI.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", LLMUsage{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", LLMUsage{}, fmt.Errorf("openai chat http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return "", LLMUsage{}, err
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", LLMUsage{}, fmt.Errorf("openai: empty completion")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), LLMUsage{
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
	}, nil
}

func openAIChatCompletionsURL() string {
	// Reuse test override from openai package when set.
	return "https://api.openai.com/v1/chat/completions"
}

// BuildCoachQualityReport scores structured daily feedback for live A/B tests.
func BuildCoachQualityReport(persona CoachPersona, out *CoachStructuredOutput, withMemory bool) CoachQualityReport {
	text := FlattenCoachOutput(out)
	pers := ScoreCoachPersonalization(persona, out, withMemory)

	specificity := pers.Score
	if withMemory && len(persona.WantWithMemoryOnly) > 0 {
		specificity = (pers.Score + pers.MemoryOnlyScore) / 2
	}

	supportive := outputHasSupportiveTone(text)
	encourage := outputHasEncouragement(text)
	safety := outputHasSafety(out, text)

	composite := coachCompositeScore(
		pers.Score,
		pers.MemoryOnlyScore,
		withMemory,
		pers.HasHistoryCallback,
		pers.MentionsAdherence,
		supportive,
		encourage,
		safety,
		len(pers.HitAvoid) == 0,
		specificity,
	)

	return CoachQualityReport{
		Persona:          pers,
		SupportiveTone:   supportive,
		Encouragement:    encourage,
		HasSafety:        safety,
		SpecificityScore: specificity,
		CompositeScore:   composite,
	}
}

// BuildRoutineQualityReport scores suggested routine output.
func BuildRoutineQualityReport(persona CoachPersona, r SuggestedRoutine, withMemory bool) RoutineQualityReport {
	text := FlattenSuggestedRoutine(r)
	pers := ScoreRoutinePersonalization(persona, r, withMemory)
	supportive := outputHasSupportiveTone(text)
	encourage := outputHasEncouragement(text)
	safety := strings.Contains(text, "patch") || strings.Contains(text, "bác sĩ") ||
		strings.Contains(text, "dịu") || len(r.SafetyNotes) > 0

	composite := coachCompositeScore(
		pers.Score, 0, withMemory, false, strings.Contains(text, "routine") || strings.Contains(text, "bước"),
		supportive, encourage, safety, len(pers.HitAvoid) == 0, pers.Score,
	)

	return RoutineQualityReport{
		Persona:        pers,
		SupportiveTone: supportive,
		Encouragement:  encourage,
		HasSafety:      safety,
		CompositeScore: composite,
	}
}

func coachCompositeScore(
	persScore, memScore float64,
	withMemory, historyCB, adherence, supportive, encourage, safety, noAvoidHits bool,
	specificity float64,
) float64 {
	var sum, w float64
	add := func(v float64, weight float64) {
		sum += v * weight
		w += weight
	}
	add(persScore, 2)
	if withMemory {
		add(memScore, 1.5)
	}
	if historyCB {
		add(1, 1)
	} else if withMemory {
		add(0, 1)
	}
	if adherence {
		add(1, 1)
	}
	if supportive {
		add(1, 0.8)
	}
	if encourage {
		add(1, 0.8)
	}
	if safety {
		add(1, 0.6)
	}
	if noAvoidHits {
		add(1, 1.2)
	} else {
		add(0, 1.2)
	}
	add(specificity, 1.5)
	if w == 0 {
		return 0
	}
	return sum / w
}

func outputHasSupportiveTone(text string) bool {
	for _, phrase := range []string{
		"em ", "bạn ", "mình", "đừng lo", "hiểu", "ổn", "nhẹ nhàng", "dịu", "cùng",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func outputHasEncouragement(text string) bool {
	for _, phrase := range []string{
		"tiếp tục", "giỏi", "tốt lắm", "kiên trì", "đáng khen", "cố lên", "khen", "effort", "duy trì",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func outputHasSafety(out *CoachStructuredOutput, text string) bool {
	if out != nil && (len(out.SafetyReminders) > 0 || strings.TrimSpace(out.MedicalDisclaimer) != "") {
		return true
	}
	for _, phrase := range []string{"bác sĩ", "patch test", "an toàn", "dịu", "ngừng"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
