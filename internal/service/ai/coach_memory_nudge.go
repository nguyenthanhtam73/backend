package ai

import (
	"fmt"
	"strings"
)

// prependCoachActionPriority surfaces the COACH_ACTION line before USER_CONTEXT so
// small models do not bury it under long memory blocks.
func prependCoachActionPriority(userContext string) string {
	for _, line := range strings.Split(userContext, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "- COACH_ACTION:") {
			action := strings.TrimSpace(strings.TrimPrefix(trim, "- COACH_ACTION:"))
			return "⚠️ PRIORITY — apply in strengths[0] OR summary_notes: " + action + "\n\n"
		}
	}
	return ""
}

// coachMemoryTurnChecklist appends a short required checklist when USER_MEMORY
// blocks are present so the model cannot miss adherence / callback rules.
func coachMemoryTurnChecklist(userContext string) string {
	return coachTurnChecklist(userContext, false)
}

// coachTurnChecklist appends required pre-flight checks before JSON output.
// hasVision is true when VISION_SUMMARY_JSON was produced successfully for this turn.
func coachTurnChecklist(userContext string, hasVision bool) string {
	if strings.Contains(userContext, "no saved memory yet") && !hasVision {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nCOACH CHECKLIST (required — verify before JSON):\n")
	if hasVision {
		b.WriteString("- ≥4–5 photo details (region+cue+degree/count) — MUST open \"Mày thấy hôm nay…\" OR \"Mình thấy hôm nay…\" OR \"Trên ảnh mình thấy vùng …\" OR \"Có … nốt mụn/chấm thâm ở …\"; NO lists/report tone.\n")
		b.WriteString("- BAN ALL vague: \"da hỗn hợp\", \"da dễ nổi mụn\", \"dễ nổi mụn\", \"da hơi khô\", \"sản phẩm nhẹ nhàng\", \"chăm sóc nhẹ\".\n")
	}
	if strings.Contains(userContext, "## Recent SkinChecks") {
		b.WriteString("- HISTORY (MANDATORY): ≥1 \"So với lần trước…\" callback in situation_analysis.\n")
	}
	if hasVision || strings.Contains(userContext, "## Recent SkinChecks") {
		b.WriteString("- EMOTION: warm sincere praise OR playful tease + gentle closing — never cold/clinical.\n")
		b.WriteString("- TIPS: concrete step+region+role — NOT vague product advice.\n")
		if MaxCoachValidationRetries > 0 {
			b.WriteString(fmt.Sprintf("- Self-check: ≥4 vision specifics · zero banned phrases · validation will retry up to %d× if vague.\n", MaxCoachValidationRetries))
		} else {
			b.WriteString("- Self-check BEFORE output (no second chance): ≥4 vision specifics · zero banned phrases · concrete tips.\n")
		}
	}
	if strings.Contains(userContext, "## Routine adherence") {
		b.WriteString("- strengths OR summary_notes: MUST mention routine adherence per COACH_ACTION (praise / simplify / encourage — no guilt).\n")
		b.WriteString("- routine_hints: count MUST match COACH_ACTION tier (low → max 3 lines; none → max 2).\n")
	}
	if strings.Contains(userContext, "## Past AI feedback votes") || strings.Contains(userContext, "## Feedback summary") {
		b.WriteString("- Do NOT repeat angles user marked 👎 in feedback history.\n")
	}
	return b.String()
}

func needsAdherenceRetry(userContext string, out *CoachStructuredOutput) bool {
	if !strings.Contains(userContext, "COACH_ACTION:") {
		return false
	}
	return !outputMentionsAdherence(FlattenCoachOutput(out))
}

func needsVisionDetailRetry(visionRaw string, out *CoachStructuredOutput) bool {
	if strings.TrimSpace(visionRaw) == "" {
		return false
	}
	if CountVisionDetailCitations(visionRaw, out) < MinVisionDetailCitations {
		return true
	}
	return needsNaturalToneRetry(out)
}

// needsCoachOutputRetry validates vision specificity, history callback, tone, and tip concreteness.
func needsCoachOutputRetry(visionRaw, userContext string, out *CoachStructuredOutput) bool {
	if strings.TrimSpace(visionRaw) != "" {
		if CountVisionDetailCitations(visionRaw, out) < MinVisionDetailCitations {
			return true
		}
		if !outputHasRequiredVisionOpener(out) {
			return true
		}
	}
	if strings.Contains(userContext, "## Recent SkinChecks") && !outputHasHistoryCallback(FlattenCoachOutput(out)) {
		return true
	}
	if needsNaturalToneRetry(out) {
		return true
	}
	return outputHasVagueTipPhrases(out) || outputHasBannedGenericLabels(FlattenCoachOutput(out))
}

// coachOutputRetryPrompt builds a validation failure appendix for coach re-generation.
func coachOutputRetryPrompt(visionRaw, userContext string, attempt int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n\nVALIDATION FAILED (attempt %d/%d): Regenerate the FULL JSON.\n", attempt, MaxCoachValidationRetries))
	b.WriteString(fmt.Sprintf("- ≥%d photo-specific details with region+cue+degree (counts OK: \"2-3 nốt đỏ ở cằm\").\n", MinVisionDetailCitations))
	b.WriteString("- MUST open situation_analysis with \"Mày thấy hôm nay…\" OR \"Mình thấy hôm nay…\" OR \"Trên ảnh mình thấy vùng …\" OR \"Có … nốt mụn/chấm thâm ở …\".\n")
	if strings.Contains(userContext, "## Recent SkinChecks") {
		b.WriteString("- MUST include \"So với lần trước…\" history callback.\n")
	}
	b.WriteString("- BAN: \"da hỗn hợp\", \"da dễ nổi mụn\", \"dễ nổi mụn\", \"sản phẩm nhẹ nhàng\", vague dryness without region.\n")
	b.WriteString("- Tips must be concrete (step + region + product role). Warm opener/closing. NO report tone.\n")
	if strings.TrimSpace(visionRaw) != "" {
		b.WriteString("- Weave cues from VISION_SUMMARY_JSON — do not invent details not in photo.\n")
	}
	return b.String()
}

func needsNaturalToneRetry(out *CoachStructuredOutput) bool {
	if out == nil {
		return true
	}
	nat := ScoreCoachNaturalness(out)
	if nat.HasReportLikeTone || !nat.HasConversationalOpener {
		return true
	}
	if !nat.HasWarmEncouragement || !nat.HasWarmOpening || !nat.HasWarmClosing {
		return true
	}
	return outputHasGenericSkinPhrases(FlattenCoachOutput(out))
}
