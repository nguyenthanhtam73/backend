package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// DailyFeedbackInput bundles context for a text-only coach turn with memory.
type DailyFeedbackInput struct {
	UserID       uuid.UUID
	Deps         UserMemoryDeps
	TodayContext string // profile + notes for this turn (required)
	SkillLevel   string
	Check        *domain.SkinCheck  // optional — resolves skill level
	Profile      *domain.SkinProfile // optional — resolves skill level
	MemOpts      UserMemoryOptions
}

// GenerateDailyFeedbackWithMemory builds USER_MEMORY and runs the coach (Claude
// preferred; OpenAI JSON fallback). Vision is never used here.
func GenerateDailyFeedbackWithMemory(ctx context.Context, cfg *config.Config, in DailyFeedbackInput) (*CoachStructuredOutput, error) {
	if in.UserID == uuid.Nil {
		return nil, fmt.Errorf("ai daily feedback: user id required")
	}
	today := strings.TrimSpace(in.TodayContext)
	if today == "" {
		return nil, fmt.Errorf("ai daily feedback: today context required")
	}

	memory, memDebug := BuildUserMemoryWithDebug(ctx, in.UserID, in.Deps, in.MemOpts)
	LogMemoryInjection("daily-feedback", in.UserID, uuid.Nil, memDebug)

	var userCtx strings.Builder
	userCtx.WriteString(today)
	userCtx.WriteString("\n\n")
	userCtx.WriteString(memory)

	skill := strings.TrimSpace(in.SkillLevel)
	if skill == "" {
		skill = ResolveCoachSkillLevel(in.Check, in.Profile)
	}
	return GenerateDailyFeedback(ctx, cfg, userCtx.String(), skill)
}

// LogMemoryInjection emits a structured debug line when USER_MEMORY is wired
// into an AI call. Pass skinCheckID when the turn is tied to a check-in row.
func LogMemoryInjection(pipeline string, userID, skinCheckID uuid.UUID, d MemoryDebug) {
	args := []any{
		"pipeline", pipeline,
		"user_id", userID.String(),
		"chars", d.CharCount,
		"sections", strings.Join(d.SectionsPresent, ","),
		"recent_checks", d.RecentChecks,
		"feedback_helpful", d.HelpfulVotes,
		"feedback_not_helpful", d.NotHelpfulVotes,
		"adherence", d.AdherenceTier,
		"cache_hit", d.CacheHit,
		"prompt_version", CoachDailyPromptVersion,
	}
	if skinCheckID != uuid.Nil {
		args = append(args, "skin_check_id", skinCheckID.String())
	}
	slog.Debug("user_memory injected", args...)
}

// GenerateDailyFeedback produces structured coach JSON without vision.
// userContextMarkdown should include USER_MEMORY when personalisation is desired.
// skillLevel: "beginner" | "intermediate" | "advanced".
func GenerateDailyFeedback(ctx context.Context, cfg *config.Config, userContextMarkdown string, skillLevel string) (*CoachStructuredOutput, error) {
	if cfg == nil {
		return nil, fmt.Errorf("ai daily feedback: config required")
	}
	u := strings.TrimSpace(userContextMarkdown)
	if u == "" {
		return nil, fmt.Errorf("ai daily feedback: user context required")
	}
	client := &http.Client{Timeout: 4 * time.Minute}

	system, textBody := buildDailyFeedbackPrompt(u, skillLevel)

	out, err := callDailyFeedbackLLM(ctx, cfg, client, system, textBody)
	if err != nil {
		return nil, err
	}
	out.ProductSuggestions = FinalizeProductSuggestions(out.ProductSuggestions, u)
	if needsAdherenceRetry(u, out) {
		retryBody := textBody + "\n\nVALIDATION FAILED: your JSON did not mention routine adherence in strengths or summary_notes. Regenerate the FULL JSON — include one sentence about routine ticks/effort per COACH_ACTION.\n"
		if retryOut, retryErr := callDailyFeedbackLLM(ctx, cfg, client, system, retryBody); retryErr == nil && retryOut != nil {
			retryOut.ProductSuggestions = FinalizeProductSuggestions(retryOut.ProductSuggestions, u)
			out = retryOut
		}
	}
	return out, nil
}

func buildDailyFeedbackPrompt(userContextMarkdown, skillLevel string) (system, user string) {
	u := strings.TrimSpace(userContextMarkdown)
	var userMsg strings.Builder
	userMsg.WriteString("The user did not attach new photos for this turn. Base your coaching ONLY on USER_CONTEXT below (and acknowledge you have no fresh vision cues).\n\n")
	if priority := prependCoachActionPriority(u); priority != "" {
		userMsg.WriteString(priority)
	}
	userMsg.WriteString("USER_CONTEXT:\n")
	userMsg.WriteString(u)
	userMsg.WriteString(coachMemoryTurnChecklist(u))
	AppendAffiliateCoachContext(&userMsg)
	userMsg.WriteString("\n\nNow produce the FINAL coach output as ONE JSON object matching this schema exactly.\n\n")
	userMsg.WriteString(CoachOutputJSONSchemaBlock)

	skill := strings.TrimSpace(skillLevel)
	if skill == "" {
		skill = "intermediate"
	}
	return GetCoachPrompt(skill), userMsg.String()
}

func callDailyFeedbackLLM(ctx context.Context, cfg *config.Config, client *http.Client, system, textBody string) (*CoachStructuredOutput, error) {
	result, err := TextCoachCompletion(ctx, cfg, client, "daily-feedback", system, textBody)
	if err != nil {
		return nil, err
	}
	slog.Debug("daily feedback llm",
		"provider", result.Provider,
		"model", result.Model,
		"fallback", result.Fallback,
	)
	return parseCoachStructuredOutput(result.Text, "ai daily feedback")
}

func parseCoachStructuredOutput(text, label string) (*CoachStructuredOutput, error) {
	raw, err := ExtractJSONObject(text)
	if err != nil {
		return nil, err
	}
	var out CoachStructuredOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: parse json: %w", label, err)
	}
	out.ProductSuggestions = SanitizeProductSuggestions(out.ProductSuggestions)
	LogCoachOutput(label, "", &out)
	return &out, nil
}
