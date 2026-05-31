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
	VisionDetailCount  int
	HasGenericPhrases  bool
	HasNaturalTone     bool
	HasReportLikeTone  bool
	HasWarmOpening     bool
	HasWarmClosing     bool
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
		HasGenericPhrases:  outputHasGenericSkinPhrases(text),
	}
	if persona.VisionJSON != "" {
		res.VisionDetailCount = CountVisionDetailCitations(persona.VisionJSON, out)
	}
	nat := ScoreCoachNaturalness(out)
	res.HasNaturalTone = nat.HasNaturalTone
	res.HasReportLikeTone = nat.HasReportLikeTone
	res.HasWarmOpening = nat.HasWarmOpening
	res.HasWarmClosing = nat.HasWarmClosing
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
		"tuần trước", "mấy hôm", "lần gần", "so với",
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

// CoachNaturalnessResult scores conversational warmth vs report-like tone.
type CoachNaturalnessResult struct {
	HasNaturalTone          bool
	HasConversationalOpener bool
	HasReportLikeTone       bool
	HasWarmOpening          bool
	HasWarmClosing          bool
	HasWarmEncouragement    bool
	NaturalnessScore        float64
	EmotionalScore          float64
}

// ScoreCoachNaturalness evaluates v17 warm chat persona on structured coach output.
func ScoreCoachNaturalness(out *CoachStructuredOutput) CoachNaturalnessResult {
	if out == nil {
		return CoachNaturalnessResult{}
	}
	text := FlattenCoachOutput(out)
	situation := strings.ToLower(out.SituationAnalysis)
	res := CoachNaturalnessResult{
		HasNaturalTone:          outputHasNaturalTone(text),
		HasConversationalOpener: outputHasConversationalOpener(situation),
		HasReportLikeTone:       outputHasReportLikePhrases(situation),
		HasWarmOpening:          outputHasWarmOpening(out),
		HasWarmClosing:          outputHasWarmClosing(out),
		HasWarmEncouragement:    outputHasWarmEncouragement(text),
	}
	score := 0.0
	if res.HasNaturalTone {
		score += 0.20
	}
	if res.HasConversationalOpener {
		score += 0.20
	}
	if res.HasWarmOpening {
		score += 0.15
	}
	if res.HasWarmClosing {
		score += 0.15
	}
	if res.HasWarmEncouragement {
		score += 0.15
	}
	if !res.HasReportLikeTone {
		score += 0.15
	}
	res.NaturalnessScore = score

	emo := 0.0
	if res.HasWarmOpening {
		emo += 0.30
	}
	if res.HasWarmEncouragement {
		emo += 0.35
	}
	if res.HasWarmClosing {
		emo += 0.20
	}
	if res.HasConversationalOpener {
		emo += 0.15
	}
	res.EmotionalScore = emo
	return res
}

func outputHasConversationalOpener(situation string) bool {
	for _, phrase := range []string{
		"mình thấy", "nhìn ảnh", "hôm nay da bạn", "trên ảnh", "mình nhìn", "nhìn vào ảnh",
	} {
		if strings.Contains(situation, phrase) {
			return true
		}
	}
	return false
}

func outputHasNaturalTone(text string) bool {
	hits := 0
	for _, phrase := range []string{
		"mình", "bạn ", "nhé", "nha", "thấy", "nghe", "hơi", "một chút", "chút",
		"cũng", "đấy", "đó", " ạ", "nè", "hen", "biết", "cùng",
	} {
		if strings.Contains(text, phrase) {
			hits++
		}
	}
	return hits >= 2
}

func outputHasReportLikePhrases(text string) bool {
	for _, phrase := range []string{
		"phân tích cho thấy", "tình trạng da hiện tại", "kết luận:", "đánh giá tổng quan",
		"nhìn chung tình trạng", "báo cáo", "tóm lại tình trạng", "dấu hiệu cho thấy da",
		"1.", "2.", "3.", "t-zone:", "má:", "cằm:",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func outputHasWarmOpening(out *CoachStructuredOutput) bool {
	if len(out.Strengths) == 0 {
		return false
	}
	opening := strings.ToLower(strings.Join(out.Strengths, " "))
	for _, phrase := range []string{
		"tốt lắm", "giỏi", "cố lên", "đáng khen", "kiên trì", "effort", "khó nhất",
		"biết", "khen", "cảm ơn", "tuyệt", "ổn", "ok", "đều", "tick", "chụp", "ghi",
	} {
		if strings.Contains(opening, phrase) {
			return true
		}
	}
	return len(out.Strengths) > 0
}

func outputHasWarmClosing(out *CoachStructuredOutput) bool {
	closing := strings.ToLower(strings.TrimSpace(out.SummaryNotes))
	if closing == "" {
		return false
	}
	for _, phrase := range []string{
		"mai ", "nhé", "nha", "mình", "cùng", "muốn xem", "chụp", "dịu", "theo dõi", "cố gắng",
	} {
		if strings.Contains(closing, phrase) {
			return true
		}
	}
	return len(closing) > 20
}

func outputHasWarmEncouragement(text string) bool {
	for _, phrase := range []string{
		"bạn đang làm khá tốt", "bạn đang làm tốt", "bạn đang làm rất tốt", "đang làm khá tốt",
		"mình khuyên thật lòng", "thật lòng nhé", "khá tốt rồi", "tốt lắm", "đáng khen",
		"cố lên", "kiên trì", "mình biết phần này không dễ", "cảm ơn bạn", "đang cố gắng",
		"đang đi đúng hướng", "mình trân trọng",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
// appear in coach output (situation_analysis + concern_alignment).
func CountVisionDetailCitations(visionJSON string, out *CoachStructuredOutput) int {
	if out == nil || strings.TrimSpace(visionJSON) == "" {
		return 0
	}
	text := strings.ToLower(out.SituationAnalysis + " " + out.ConcernAlignment)
	phrases := extractVisionObservationPhrases(visionJSON)
	matched := 0
	seen := make(map[string]struct{})
	for _, phrase := range phrases {
		p := strings.ToLower(strings.TrimSpace(phrase))
		if p == "" || len([]rune(p)) < 4 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		if strings.Contains(text, p) {
			seen[p] = struct{}{}
			matched++
		}
	}
	if matched < MinVisionDetailCitations {
		matched = countRegionCueDetails(text)
	}
	return matched
}

func extractVisionObservationPhrases(visionJSON string) []string {
	type visionPayload struct {
		VisibleObservations        []string `json:"visible_observations"`
		TextureAndOilCues          string   `json:"texture_and_oil_cues"`
		RednessOrDiscolorationCues string   `json:"redness_or_discoloration_cues"`
	}
	var payload visionPayload
	if err := json.Unmarshal([]byte(visionJSON), &payload); err != nil {
		return splitObservationClauses(visionJSON)
	}
	var phrases []string
	phrases = append(phrases, payload.VisibleObservations...)
	if s := strings.TrimSpace(payload.TextureAndOilCues); s != "" {
		phrases = append(phrases, splitObservationClauses(s)...)
	}
	if s := strings.TrimSpace(payload.RednessOrDiscolorationCues); s != "" {
		phrases = append(phrases, splitObservationClauses(s)...)
	}
	return phrases
}

func splitObservationClauses(s string) []string {
	s = strings.ReplaceAll(s, "—", ",")
	s = strings.ReplaceAll(s, ";", ",")
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// countRegionCueDetails counts clauses that pair a face region with a visible cue.
func countRegionCueDetails(text string) int {
	regions := []string{
		"t-zone", "t zone", "trán", "mũi", "cằm", "má", "vùng mắt", "quanh miệng",
		"má trái", "má phải", "forehead", "cheek", "chin", "nose",
	}
	cues := []string{
		"bóng", "dầu", "khô", "đỏ", "mụn", "thâm", "sẩn", "lỗ chân lông",
		"viêm", "flak", "matte", "shiny", "red", "bump", "pore", "dark mark",
	}
	count := 0
	for _, region := range regions {
		if !strings.Contains(text, region) {
			continue
		}
		for _, cue := range cues {
			if strings.Contains(text, cue) {
				count++
				break
			}
		}
	}
	return count
}

func outputHasGenericSkinPhrases(text string) bool {
	for _, phrase := range []string{
		"da bạn hơi khô",
		"da hơi khô",
		"da không đều màu",
		"da cần dưỡng ẩm",
		"cần chăm sóc thêm",
		"da cần được chăm sóc",
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
