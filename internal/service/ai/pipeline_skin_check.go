package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

// RunSkinCheckCoach runs the preferred pipeline: GPT vision (observations) → Claude (coach JSON).
// If Anthropic is not configured, falls back to a single GPT-4o multimodal coach call (same JSON schema).
//
// userMemory is an optional pre-built memory block from BuildUserMemoryContext
// (long-term context for this user: full profile + recent check-ins + prior
// AI feedback + routine adherence). Empty string is OK — pipeline simply
// runs without long-term memory and relies on today's context only.
func RunSkinCheckCoach(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	uploadRoot string,
	check *domain.SkinCheck,
	profile *domain.SkinProfile,
	userMemory string,
) (out *CoachStructuredOutput, pipelineModelVersion string, err error) {
	if cfg == nil || check == nil {
		return nil, "", fmt.Errorf("ai: invalid input")
	}
	urls, err := dto.DecodeStringSlice(check.ImageURLs)
	if err != nil || len(urls) == 0 {
		return nil, "", fmt.Errorf("ai: no image paths")
	}
	skill := ResolveCoachSkillLevel(check, profile)
	system := GetCoachPrompt(skill)

	if strings.TrimSpace(cfg.Anthropic.APIKey) != "" {
		// Vision pass is best-effort: if OpenAI Vision fails (quota, network, malformed
		// image), still produce coach feedback from text-only context. The model receives
		// an explicit "no fresh vision cues" note so it acknowledges the limitation
		// instead of hallucinating photo observations.
		visionRaw, vErr := VisionObservationPass(ctx, cfg, httpClient, uploadRoot, urls)
		visionStatus := "ok"
		if vErr != nil {
			visionRaw = ""
			visionStatus = "unavailable"
		}

		userMsg := strings.Builder{}
		if visionStatus == "ok" {
			userMsg.WriteString("The following VISION_SUMMARY_JSON was produced by a separate vision-only pass over the user's check-in photos. It is NOT a diagnosis — only soft visual cues.\n\n")
			userMsg.WriteString("VISION_SUMMARY_JSON:\n")
			userMsg.WriteString(visionRaw)
		} else {
			userMsg.WriteString("VISION_SUMMARY_JSON: <unavailable for this turn — the separate vision pass could not run cleanly. Coach based on TODAY_CHECK_IN + RECENT_DIARY only, and acknowledge that no fresh photo cues are available in concern_alignment.>")
		}
		userMsg.WriteString("\n\nUSER_CONTEXT (saved profile + today's self-report + environment):\n")
		userMsg.WriteString(BuildDailyCheckInCoachContext(check, profile))
		if s := strings.TrimSpace(userMemory); s != "" {
			userMsg.WriteString("\n")
			userMsg.WriteString(s)
		}
		userMsg.WriteString("\n\nNow produce the FINAL coach output as ONE JSON object matching this schema exactly.\n\n")
		userMsg.WriteString(CoachOutputJSONSchemaBlock)

		coachRaw, cErr := AnthropicMessages(ctx, cfg, httpClient, system, userMsg.String())
		if cErr != nil {
			return nil, "", fmt.Errorf("claude coach: %w", cErr)
		}
		if strings.TrimSpace(coachRaw) == "" {
			return nil, "", fmt.Errorf("claude returned empty response — check Anthropic quota or model availability")
		}
		raw, ej := ExtractJSONObject(coachRaw)
		if ej != nil {
			return nil, "", fmt.Errorf("extract json: %w", ej)
		}
		if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
			return nil, "", fmt.Errorf("claude returned non-JSON text: %.200q", coachRaw)
		}
		var parsed CoachStructuredOutput
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, "", fmt.Errorf("parse coach json: %w (first 200 chars: %.200q)", err, coachRaw)
		}
		visionModel := strings.TrimSpace(cfg.OpenAI.VisionModel)
		if visionModel == "" {
			visionModel = strings.TrimSpace(cfg.OpenAI.Model)
		}
		if visionModel == "" {
			visionModel = "gpt-4o"
		}
		coachModel := strings.TrimSpace(cfg.Anthropic.Model)
		if coachModel == "" {
			coachModel = "claude-sonnet-4-20250514"
		}
		ver := fmt.Sprintf("pipeline=anthropic+gpt_vision|vision=%s(%s)|coach=%s", visionModel, visionStatus, coachModel)
		return &parsed, ver, nil
	}

	fullUser := BuildDailyCheckInCoachContext(check, profile)
	if s := strings.TrimSpace(userMemory); s != "" {
		fullUser += "\n" + s
	}
	fullUser += "\n\n" + CoachOutputJSONSchemaBlock
	rawText, ferr := GPTSinglePassSkinCoach(ctx, cfg, httpClient, uploadRoot, urls, fullUser, skill)
	if ferr != nil {
		return nil, "", ferr
	}
	// Surface empty responses with a clearer message than "unexpected end of JSON
	// input" — this saves users from chasing JSON-parse red herrings when the real
	// problem is upstream (e.g. invalid image bytes, exhausted quota, safety filter).
	if strings.TrimSpace(rawText) == "" {
		return nil, "", fmt.Errorf("coach returned empty response — check image validity and OpenAI quota")
	}
	raw, ej := ExtractJSONObject(rawText)
	if ej != nil {
		return nil, "", ej
	}
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return nil, "", fmt.Errorf("coach returned non-JSON text: %.200q", rawText)
	}
	var parsed CoachStructuredOutput
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, "", fmt.Errorf("parse coach json: %w (first 200 chars: %.200q)", err, rawText)
	}
	vm := strings.TrimSpace(cfg.OpenAI.VisionModel)
	if vm == "" {
		vm = strings.TrimSpace(cfg.OpenAI.Model)
	}
	if vm == "" {
		vm = "gpt-4o"
	}
	return &parsed, fmt.Sprintf("pipeline=gpt_multimodal|model=%s", vm), nil
}
