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
		b.WriteString("- ≥4 photo details (region+cue) woven naturally; open \"mình thấy\" / \"hôm nay da bạn\"; NO lists/report tone.\n")
		b.WriteString("- BAN: \"da hơi khô\", \"cần dưỡng ẩm\" without region.\n")
	}
	if strings.Contains(userContext, "## Recent SkinChecks") {
		b.WriteString("- MUST include \"So với lần trước…\" / \"Vài hôm trước…\" callback.\n")
	}
	if hasVision || strings.Contains(userContext, "## Recent SkinChecks") {
		b.WriteString("- Warm praise in strengths; weave \"mình khuyên thật lòng nhé\" or \"bạn đang làm khá tốt rồi đó\" naturally; gentle closing in summary_notes; keep copy concise.\n")
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
