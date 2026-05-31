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
		b.WriteString("- situation_analysis/concern_alignment: cite ≥3–4 photo-specific details (region + cue + degree). Open with \"mình thấy\" / \"hôm nay da bạn\" — weave naturally, NO numbered lists or \"T-zone:\" headers.\n")
		b.WriteString("- BAN vague-only: \"da hơi khô\", \"da cần dưỡng ẩm\", \"da không đều màu\" without naming a region.\n")
		b.WriteString("- BAN report tone: \"Phân tích cho thấy\", \"Tình trạng da hiện tại\", \"1. 2. 3.\" lists.\n")
	}
	if strings.Contains(userContext, "## Recent SkinChecks") {
		b.WriteString("- situation_analysis: MUST include ≥1 history callback starting with \"So với…\" / \"Vài hôm trước…\" / \"Mấy lần gần đây…\".\n")
	}
	if hasVision || strings.Contains(userContext, "## Recent SkinChecks") {
		b.WriteString("- strengths: warm sincere praise (effort, not appearance). summary_notes: gentle closing + tomorrow focus.\n")
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

func needsNaturalToneRetry(out *CoachStructuredOutput) bool {
	if out == nil {
		return true
	}
	nat := ScoreCoachNaturalness(out)
	if nat.HasReportLikeTone || !nat.HasConversationalOpener {
		return true
	}
	return outputHasGenericSkinPhrases(FlattenCoachOutput(out))
}
