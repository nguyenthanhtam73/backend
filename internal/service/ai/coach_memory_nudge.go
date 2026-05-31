package ai

import "strings"

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
		b.WriteString("- ≥4–5 photo details (region+cue+degree) woven naturally; MUST open with \"Mình thấy hôm nay…\" OR \"Trên ảnh mình thấy…\" OR \"Vùng … của bạn…\"; NO lists/report tone.\n")
		b.WriteString("- BAN vague skin/tips: \"da hơi khô\", \"cần dưỡng ẩm\", \"sản phẩm nhẹ nhàng\", \"chăm sóc nhẹ\" without region+action.\n")
	}
	if strings.Contains(userContext, "## Recent SkinChecks") {
		b.WriteString("- HISTORY (MANDATORY): ≥1 \"So với lần trước…\" / \"Vài hôm trước…\" callback in situation_analysis.\n")
	}
	if hasVision || strings.Contains(userContext, "## Recent SkinChecks") {
		b.WriteString("- EMOTION (HIGH PRIORITY): warm sincere praise in strengths; gentle encouragement in summary_notes — never cold/clinical.\n")
		b.WriteString("- TIPS (MANDATORY): improvements/routine_hints must be concrete (step + region + product role) — NOT \"sản phẩm nhẹ nhàng\".\n")
		b.WriteString("- Self-check before JSON: ≥4 vision details · history callback (if memory) · warm opener+closing · specific tips.\n")
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
	}
	if strings.Contains(userContext, "## Recent SkinChecks") && !outputHasHistoryCallback(FlattenCoachOutput(out)) {
		return true
	}
	if needsNaturalToneRetry(out) {
		return true
	}
	return outputHasVagueTipPhrases(out)
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
