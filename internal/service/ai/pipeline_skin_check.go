package ai

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

// RunSkinCheckCoach runs hybrid coaching: GPT-4o vision (observations) → Claude Sonnet
// text coach JSON, with GPT-4o text fallback when Claude is unavailable or errors.
//
// userMemory is an optional pre-built memory block from BuildUserMemoryContext.
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
	if !cfg.HasOpenAIKey() && !cfg.HasAnthropicKey() {
		return nil, "", fmt.Errorf("ai: configure DADIARY_OPENAI_API_KEY (vision) and/or DADIARY_ANTHROPIC_API_KEY (coach)")
	}
	urls, err := dto.DecodeStringSlice(check.ImageURLs)
	if err != nil || len(urls) == 0 {
		return nil, "", fmt.Errorf("ai: no image paths")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTextCoachHTTPTimeout}
	}

	skill := ResolveCoachSkillLevel(check, profile)
	system := GetCoachPrompt(skill)

	visionRaw := ""
	visionStatus := "skipped"
	if cfg.HasOpenAIKey() {
		var vErr error
		visionRaw, vErr = VisionObservationPass(ctx, cfg, httpClient, uploadRoot, urls)
		if vErr != nil {
			visionRaw = ""
			visionStatus = "unavailable"
			slog.Warn("skin-check: vision pass failed", "err", vErr)
		} else {
			visionStatus = "ok"
		}
	} else {
		visionStatus = "no_openai_key"
	}

	fullCtx := BuildDailyCheckInCoachContext(check, profile)
	if s := strings.TrimSpace(userMemory); s != "" {
		fullCtx += "\n" + s
	}

	userMsg := buildSkinCheckCoachUserMessage(check, profile, userMemory, visionRaw, visionStatus, fullCtx)
	coachResult, cErr := TextCoachCompletion(ctx, cfg, httpClient, "skin-check", system, userMsg)
	if cErr != nil {
		return nil, "", cErr
	}

	parsed, pErr := parseCoachStructuredOutput(coachResult.Text, "skin-check")
	if pErr != nil {
		return nil, "", pErr
	}
	if visionStatus == "ok" {
		retryBody := userMsg
		for attempt := 1; attempt <= MaxCoachValidationRetries && needsCoachOutputRetry(visionRaw, fullCtx, parsed); attempt++ {
			retryBody = userMsg + coachOutputRetryPrompt(visionRaw, fullCtx, attempt)
			retryResult, retryErr := TextCoachCompletion(ctx, cfg, httpClient, fmt.Sprintf("skin-check-retry-%d", attempt), system, retryBody)
			if retryErr != nil {
				slog.Warn("skin-check: coach validation retry failed", "attempt", attempt, "err", retryErr)
				break
			}
			retryOut, retryParseErr := parseCoachStructuredOutput(retryResult.Text, fmt.Sprintf("skin-check-retry-%d", attempt))
			if retryParseErr != nil {
				slog.Warn("skin-check: coach validation retry parse failed", "attempt", attempt, "err", retryParseErr)
				break
			}
			parsed = retryOut
		}
	}
	parsed.ProductSuggestions = FinalizeProductSuggestions(parsed.ProductSuggestions, fullCtx)

	ver := fmt.Sprintf(
		"pipeline=hybrid|vision=%s(%s)|coach=%s(%s%s)",
		cfg.OpenAIVisionModel(), visionStatus,
		coachResult.Model, coachResult.Provider,
		fallbackSuffix(coachResult.Fallback),
	)
	slog.Debug("skin-check coach completed",
		"vision_status", visionStatus,
		"coach_provider", coachResult.Provider,
		"coach_model", coachResult.Model,
		"coach_fallback", coachResult.Fallback,
	)
	return parsed, ver, nil
}

func buildSkinCheckCoachUserMessage(
	check *domain.SkinCheck,
	profile *domain.SkinProfile,
	userMemory, visionRaw, visionStatus, fullCtx string,
) string {
	if strings.TrimSpace(fullCtx) == "" {
		fullCtx = BuildDailyCheckInCoachContext(check, profile)
		if s := strings.TrimSpace(userMemory); s != "" {
			fullCtx += "\n" + s
		}
	}

	var userMsg strings.Builder
	switch visionStatus {
	case "ok":
		userMsg.WriteString("The following VISION_SUMMARY_JSON was produced by a separate vision-only pass over the user's check-in photos. It is NOT a diagnosis — only soft visual cues.\n\n")
		userMsg.WriteString("VISION_SUMMARY_JSON:\n")
		userMsg.WriteString(visionRaw)
	default:
		userMsg.WriteString("VISION_SUMMARY_JSON: <unavailable for this turn — the separate vision pass could not run cleanly. Coach based on TODAY_CHECK_IN + RECENT_DIARY only, and acknowledge that no fresh photo cues are available in concern_alignment.>")
	}
	if priority := prependCoachActionPriority(fullCtx); priority != "" {
		userMsg.WriteString("\n\n")
		userMsg.WriteString(priority)
	}
	userMsg.WriteString("\n\nUSER_CONTEXT (saved profile + today's self-report + environment):\n")
	userMsg.WriteString(fullCtx)
	userMsg.WriteString(coachTurnChecklist(fullCtx, visionStatus == "ok"))
	AppendAffiliateCoachContext(&userMsg)
	userMsg.WriteString("\n\nNow produce the FINAL coach output as ONE JSON object matching this schema exactly.\n\n")
	userMsg.WriteString(CoachOutputJSONSchemaBlock)
	return userMsg.String()
}

func fallbackSuffix(fallback bool) string {
	if fallback {
		return ",fallback"
	}
	return ""
}
