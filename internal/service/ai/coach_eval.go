package ai

import (
	"encoding/json"
	"log/slog"
	"strings"
)

// CoachPersonalizationResult scores how well a coach output references persona signals.
type CoachPersonalizationResult struct {
	PersonaID          string
	WithMemory         bool
	MatchedWant        []string
	MissingWant        []string
	MatchedMemoryOnly  []string
	MissingMemoryOnly  []string
	HitAvoid           []string
	Score              float64
	MemoryOnlyScore    float64
	HasHistoryCallback bool
	MentionsAdherence  bool
	RoutineHintCount   int
	OutputPreview      string
}

// FlattenCoachOutput joins all human-readable coach fields into one searchable string.
func FlattenCoachOutput(out *CoachStructuredOutput) string {
	if out == nil {
		return ""
	}
	var parts []string
	parts = append(parts, out.Strengths...)
	parts = append(parts, out.SituationAnalysis)
	for _, imp := range out.Improvements {
		parts = append(parts, imp.Tip, imp.Why)
	}
	parts = append(parts, out.RoutineHints...)
	parts = append(parts, out.AvoidOrPatch...)
	parts = append(parts, out.SafetyReminders...)
	parts = append(parts, out.ConcernAlignment, out.MedicalDisclaimer, out.SummaryNotes)
	return strings.ToLower(strings.Join(parts, " "))
}

// FlattenSuggestedRoutine joins routine suggest fields for scoring.
func FlattenSuggestedRoutine(r SuggestedRoutine) string {
	var parts []string
	for _, s := range r.Morning {
		parts = append(parts, s.Title, s.Notes)
	}
	for _, s := range r.Evening {
		parts = append(parts, s.Title, s.Notes)
	}
	parts = append(parts, r.Encouragement, r.Rationale, r.WeekNotes, r.SafetyNotes, r.ClosingReminder)
	return strings.ToLower(strings.Join(parts, " "))
}

// ScoreCoachPersonalization checks persona signals against coach JSON output.
func ScoreCoachPersonalization(persona CoachPersona, out *CoachStructuredOutput, withMemory bool) CoachPersonalizationResult {
	text := FlattenCoachOutput(out)
	res := CoachPersonalizationResult{
		PersonaID:          persona.ID,
		WithMemory:         withMemory,
		HasHistoryCallback: outputHasHistoryCallback(text),
		MentionsAdherence:  outputMentionsAdherence(text),
	}
	for _, w := range persona.WantInOutput {
		if strings.Contains(text, strings.ToLower(w)) {
			res.MatchedWant = append(res.MatchedWant, w)
		} else {
			res.MissingWant = append(res.MissingWant, w)
		}
	}
	if withMemory {
		for _, w := range persona.WantWithMemoryOnly {
			if strings.Contains(text, strings.ToLower(w)) {
				res.MatchedMemoryOnly = append(res.MatchedMemoryOnly, w)
			} else {
				res.MissingMemoryOnly = append(res.MissingMemoryOnly, w)
			}
		}
	}
	for _, a := range persona.AvoidInOutput {
		if strings.Contains(text, strings.ToLower(a)) {
			res.HitAvoid = append(res.HitAvoid, a)
		}
	}
	if len(persona.WantInOutput) > 0 {
		res.Score = float64(len(res.MatchedWant)) / float64(len(persona.WantInOutput))
	}
	if withMemory && len(persona.WantWithMemoryOnly) > 0 {
		res.MemoryOnlyScore = float64(len(res.MatchedMemoryOnly)) / float64(len(persona.WantWithMemoryOnly))
	}
	if out != nil {
		res.RoutineHintCount = len(out.RoutineHints)
		res.OutputPreview = truncateRunes(out.SituationAnalysis+" | "+out.SummaryNotes, 400)
	}
	return res
}

func outputHasHistoryCallback(text string) bool {
	for _, phrase := range []string{
		"mấy lần gần đây", "vài hôm trước", "vài hôm nay", "gần đây bạn",
		"tuần trước", "mấy hôm", "lần gần",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func outputMentionsAdherence(text string) bool {
	for _, phrase := range []string{
		"routine", "tick", "bước", "duy trì", "đều đặn", "đều ",
		"adherence", "mấy hôm bận", "mấy hôm tick", "tick ít", "rút gọn",
		"2 bước", "ít bước", "hoàn thành", "kiên trì", "duy trì routine",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// ScoreRoutinePersonalization scores a suggested routine against persona signals.
func ScoreRoutinePersonalization(persona CoachPersona, r SuggestedRoutine, withMemory bool) CoachPersonalizationResult {
	text := FlattenSuggestedRoutine(r)
	res := CoachPersonalizationResult{
		PersonaID:  persona.ID,
		WithMemory: withMemory,
	}
	for _, w := range persona.WantInOutput {
		if strings.Contains(text, strings.ToLower(w)) {
			res.MatchedWant = append(res.MatchedWant, w)
		} else {
			res.MissingWant = append(res.MissingWant, w)
		}
	}
	for _, a := range persona.AvoidInOutput {
		if strings.Contains(text, strings.ToLower(a)) {
			res.HitAvoid = append(res.HitAvoid, a)
		}
	}
	if len(persona.WantInOutput) > 0 {
		res.Score = float64(len(res.MatchedWant)) / float64(len(persona.WantInOutput))
	}
	res.OutputPreview = truncateRunes(r.Rationale+" | "+r.Encouragement, 400)
	return res
}

// LogCoachOutput emits a debug snapshot of structured coach JSON for prompt tuning.
func LogCoachOutput(pipeline, personaID string, out *CoachStructuredOutput) {
	if out == nil {
		return
	}
	slog.Debug("coach output",
		"pipeline", pipeline,
		"persona", personaID,
		"prompt_version", CoachDailyPromptVersion,
		"score", out.Score,
		"strengths", len(out.Strengths),
		"improvements", len(out.Improvements),
		"routine_hints", len(out.RoutineHints),
		"situation_analysis", truncateRunes(out.SituationAnalysis, 200),
		"summary_notes", truncateRunes(out.SummaryNotes, 160),
	)
}

// LogSuggestedRoutineOutput logs routine suggest AI output for debugging.
func LogSuggestedRoutineOutput(personaID string, r SuggestedRoutine) {
	slog.Debug("routine suggest output",
		"persona", personaID,
		"prompt_version", CoachDailyPromptVersion,
		"morning_steps", len(r.Morning),
		"evening_steps", len(r.Evening),
		"rationale", truncateRunes(r.Rationale, 200),
		"encouragement", truncateRunes(r.Encouragement, 160),
	)
}

// CoachOutputJSON returns a compact JSON snippet for test logs (truncated fields).
func CoachOutputJSON(out *CoachStructuredOutput) string {
	if out == nil {
		return "{}"
	}
	type slim struct {
		Strengths         []string `json:"strengths"`
		SituationAnalysis string   `json:"situation_analysis"`
		RoutineHints      []string `json:"routine_hints"`
		SummaryNotes      string   `json:"summary_notes"`
	}
	b, _ := json.Marshal(slim{
		Strengths:         out.Strengths,
		SituationAnalysis: out.SituationAnalysis,
		RoutineHints:      out.RoutineHints,
		SummaryNotes:      out.SummaryNotes,
	})
	return string(b)
}
