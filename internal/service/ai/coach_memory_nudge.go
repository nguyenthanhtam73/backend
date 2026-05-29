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
	if strings.Contains(userContext, "no saved memory yet") {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nCOACH CHECKLIST (required — verify before JSON):\n")
	if strings.Contains(userContext, "## Recent SkinChecks") {
		b.WriteString("- situation_analysis: include ≥1 history callback (e.g. \"mấy lần gần đây…\", \"vài hôm trước…\").\n")
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
