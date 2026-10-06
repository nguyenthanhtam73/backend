package ai

import (
	"encoding/json"
	"strings"

	"github.com/dadiary/backend/internal/dto"
	"golang.org/x/text/unicode/norm"
)

const maxZoneNotes = 5

var checkInZones = map[string]struct{}{
	"forehead": {}, "nose": {}, "left_cheek": {}, "right_cheek": {},
	"chin": {}, "around_mouth": {}, "jawline": {}, "under_eyes": {},
	"neck": {}, "other": {},
}

var checkInSeverities = map[string]struct{}{
	"mild": {}, "moderate": {}, "pronounced": {},
}

// Hard disease / diagnosis phrases. Morphology groups the product already uses
// (mụn ẩn, milia, mụn viêm, …) are not in this list. A hit drops the whole note.
var diseasePhrases = []string{
	"eczema", "rosacea", "herpes", "psoriasis", "melanoma", "carcinoma",
	"lupus", "dermatitis", "impetigo", "cellulitis", "vitiligo", "shingles",
	"chàm", "vảy nến", "vay nen", "trứng cá đỏ", "trung ca do",
	"ung thư", "ung thu", "thủy đậu", "thuy dau",
	"viêm da cơ địa", "viem da co dia",
	"chẩn đoán", "chan doan", "diagnosis", "diagnosed",
	"zona",
}

// NormalizeCheckInDetail turns the coach's raw detail fields into notes that
// are safe to store. A malformed field is dropped. A bad zone note is dropped
// whole — the text is never cut down to make it pass. evidenceKind skip clears
// zone notes. When the coach sent none and the photo is readable, notes are
// built from the vision zones.
func NormalizeCheckInDetail(zoneRaw, scoreRaw json.RawMessage, visionRaw string, photoCtx json.RawMessage, evidenceKind, locale string) ([]dto.CoachZoneNote, *dto.SkinCoachScoreNotes) {
	allowed := allowedCheckInZones(visionRaw, photoCtx)
	notes := filterZoneNotes(decodeZoneNotes(zoneRaw), allowed)
	if evidenceKind == PhotoEvidenceSkip {
		notes = nil
	} else if len(notes) == 0 && evidenceKind == PhotoEvidenceOK {
		notes = filterZoneNotes(fallbackZoneNotes(visionRaw, locale), allowed)
	}
	return notes, filterScoreNotes(decodeScoreNotes(scoreRaw))
}

// ApplyCheckInDetail writes the photo check-in detail into the skin_scores map.
// New keys sit beside the numeric gauges. They are never written under
// overall / hydration / clarity / barrier. visible_observations and
// vision_zone_observations are stored for later comparison and are not part of
// the public coach payload. Android voice is applied to the new user-facing
// strings. Both voices rewrite "hàng rào" to "lớp bảo vệ da" on those new
// strings only. A bad field is omitted; this function does not fail the analysis.
func ApplyCheckInDetail(labels map[string]any, parsed *CoachStructuredOutput, visionRaw string, photoCtx json.RawMessage, ev CheckInPhotoEvidence, clientKind, locale string) {
	if labels == nil {
		return
	}
	var zoneRaw, scoreRaw json.RawMessage
	if parsed != nil {
		zoneRaw = parsed.ZoneNotesRaw
		scoreRaw = parsed.SkinScoreNotesRaw
	}
	notes, scoreNotes := NormalizeCheckInDetail(zoneRaw, scoreRaw, visionRaw, photoCtx, ev.Kind, locale)
	android := IsAndroidCoachVoice(clientKind)
	if android {
		notes = sanitizeZoneNotes(notes)
		scoreNotes = sanitizeScoreNotes(scoreNotes)
	}
	notes = rewriteZoneBarrier(notes)
	scoreNotes = rewriteScoreBarrier(scoreNotes)
	if len(notes) > 0 {
		labels["zone_notes"] = notes
	}
	if scoreNotes != nil {
		labels["skin_score_notes"] = scoreNotes
	}
	level, needs, questions := CheckInConfidence(ev, visionRaw, photoCtx, locale)
	if android {
		questions = sanitizeQuestionList(questions)
	}
	questions = rewriteQuestionBarrier(questions)
	if level != "" {
		labels["confidence"] = level
	}
	if needs {
		labels["needs_more_info"] = true
	}
	if len(questions) > 0 {
		labels["clarify_questions"] = questions
	}
	if obs := visibleObservationStrings(visionRaw); len(obs) > 0 {
		labels["visible_observations"] = obs
	}
	if zones := zoneObservationValues(visionRaw); len(zones) > 0 {
		labels["vision_zone_observations"] = zones
	}
}

func decodeZoneNotes(raw json.RawMessage) []dto.CoachZoneNote {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	out := make([]dto.CoachZoneNote, 0, len(items))
	for _, item := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			continue
		}
		zone, okZ := jsonStringValue(obj["zone"])
		note, okN := jsonStringValue(obj["note"])
		if !okZ || !okN {
			continue
		}
		sev, _ := jsonStringValue(obj["severity"])
		out = append(out, dto.CoachZoneNote{Zone: zone, Note: note, Severity: sev})
	}
	return out
}

func decodeScoreNotes(raw json.RawMessage) *dto.SkinCoachScoreNotes {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	notes := &dto.SkinCoachScoreNotes{}
	if v, ok := jsonStringValue(obj["overall"]); ok {
		notes.Overall = v
	}
	if v, ok := jsonStringValue(obj["hydration"]); ok {
		notes.Hydration = v
	}
	if v, ok := jsonStringValue(obj["clarity"]); ok {
		notes.Clarity = v
	}
	if v, ok := jsonStringValue(obj["barrier"]); ok {
		notes.Barrier = v
	}
	return notes
}

func jsonStringValue(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func filterZoneNotes(in []dto.CoachZoneNote, allowed map[string]struct{}) []dto.CoachZoneNote {
	if len(in) == 0 {
		return nil
	}
	out := make([]dto.CoachZoneNote, 0, len(in))
	for _, n := range in {
		zone, ok := canonicalZone(n.Zone)
		if !ok {
			continue
		}
		if _, seen := allowed[zone]; !seen {
			continue
		}
		note := strings.TrimSpace(n.Note)
		if note == "" || !noteSaysLooksLike(note) || noteNamesDisease(note) {
			continue
		}
		sev, _ := canonicalSeverity(n.Severity)
		out = append(out, dto.CoachZoneNote{Zone: zone, Note: note, Severity: sev})
		if len(out) >= maxZoneNotes {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func filterScoreNotes(in *dto.SkinCoachScoreNotes) *dto.SkinCoachScoreNotes {
	if in == nil {
		return nil
	}
	out := &dto.SkinCoachScoreNotes{
		Overall:   keepScoreSentence(in.Overall),
		Hydration: keepScoreSentence(in.Hydration),
		Clarity:   keepScoreSentence(in.Clarity),
		Barrier:   keepScoreSentence(in.Barrier),
	}
	if out.Overall == "" && out.Hydration == "" && out.Clarity == "" && out.Barrier == "" {
		return nil
	}
	return out
}

func keepScoreSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || noteNamesDisease(s) {
		return ""
	}
	return s
}

func fallbackZoneNotes(visionRaw, locale string) []dto.CoachZoneNote {
	zones := zoneObservationValues(visionRaw)
	if len(zones) == 0 {
		return nil
	}
	en := strings.EqualFold(strings.TrimSpace(locale), "en")
	out := make([]dto.CoachZoneNote, 0, len(zones))
	for _, z := range zones {
		cue := strings.TrimSpace(z.Cue)
		if cue == "" {
			continue
		}
		note := cue
		if !noteSaysLooksLike(note) {
			if en {
				note = "Looks like " + cue
			} else {
				note = "Trông giống " + cue
			}
		}
		out = append(out, dto.CoachZoneNote{Zone: z.Zone, Note: note, Severity: z.Severity})
	}
	return out
}

func noteSaysLooksLike(note string) bool {
	low := strings.ToLower(note)
	for _, p := range []string{"trông giống", "trông như", "looks like", "look like"} {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

func noteNamesDisease(note string) bool {
	low := strings.ToLower(note)
	for _, p := range diseasePhrases {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

func canonicalZone(zone string) (string, bool) {
	zone = strings.ToLower(strings.TrimSpace(zone))
	if _, ok := checkInZones[zone]; !ok {
		return "", false
	}
	return zone, true
}

func canonicalSeverity(sev string) (string, bool) {
	sev = strings.ToLower(strings.TrimSpace(sev))
	if _, ok := checkInSeverities[sev]; !ok {
		return "", false
	}
	return sev, true
}

func allowedCheckInZones(visionRaw string, photoCtx json.RawMessage) map[string]struct{} {
	out := map[string]struct{}{}
	for _, z := range zoneObservationValues(visionRaw) {
		if zone, ok := canonicalZone(z.Zone); ok {
			out[zone] = struct{}{}
		}
	}
	doc := dto.DecodePhotoContext(photoCtx)
	for _, img := range doc.Images {
		if zone, ok := canonicalZone(img.Zone); ok {
			out[zone] = struct{}{}
		}
	}
	return out
}

type visionZone struct {
	Zone     string
	Cue      string
	Severity string
}

func zoneObservationValues(visionRaw string) []visionZone {
	payload, ok := decodeVisionObject(visionRaw)
	if !ok {
		return nil
	}
	raw, exists := payload["zone_observations"]
	if !exists || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	out := make([]visionZone, 0, len(items))
	for _, item := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			continue
		}
		zone, okZ := jsonStringValue(obj["zone"])
		cue, okC := jsonStringValue(obj["cue"])
		if !okZ || !okC {
			continue
		}
		sev, _ := jsonStringValue(obj["severity"])
		out = append(out, visionZone{Zone: zone, Cue: cue, Severity: sev})
	}
	return out
}

func visibleObservationStrings(visionRaw string) []string {
	payload, ok := decodeVisionObject(visionRaw)
	if !ok {
		return nil
	}
	raw, exists := payload["visible_observations"]
	if !exists || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := jsonStringValue(item)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func decodeVisionObject(visionRaw string) (map[string]json.RawMessage, bool) {
	trimmed := strings.TrimSpace(visionRaw)
	if trimmed == "" {
		return nil, false
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil || payload == nil {
		return nil, false
	}
	return payload, true
}

func sanitizeZoneNotes(in []dto.CoachZoneNote) []dto.CoachZoneNote {
	if len(in) == 0 {
		return nil
	}
	out := make([]dto.CoachZoneNote, 0, len(in))
	for _, n := range in {
		note := strings.TrimSpace(SanitizeAndroidVoiceText(n.Note))
		if note == "" {
			continue
		}
		n.Note = note
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeScoreNotes(in *dto.SkinCoachScoreNotes) *dto.SkinCoachScoreNotes {
	if in == nil {
		return nil
	}
	out := &dto.SkinCoachScoreNotes{
		Overall:   strings.TrimSpace(SanitizeAndroidVoiceText(in.Overall)),
		Hydration: strings.TrimSpace(SanitizeAndroidVoiceText(in.Hydration)),
		Clarity:   strings.TrimSpace(SanitizeAndroidVoiceText(in.Clarity)),
		Barrier:   strings.TrimSpace(SanitizeAndroidVoiceText(in.Barrier)),
	}
	if out.Overall == "" && out.Hydration == "" && out.Clarity == "" && out.Barrier == "" {
		return nil
	}
	return out
}

const barrierPhraseReplacement = "lớp bảo vệ da"

// rewriteBarrierWording replaces "hàng rào da", "hàng rào bảo vệ", and a
// standalone "hàng rào" with "lớp bảo vệ da". The match keeps the original
// capitalisation. A following "da" is absorbed so the result is never
// "lớp bảo vệ da da". Text with none of those phrases is returned unchanged.
func rewriteBarrierWording(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	parts := splitVoiceChunks(norm.NFC.String(s))
	var b strings.Builder
	changed := false
	for i := 0; i < len(parts); {
		if !parts[i].word {
			b.WriteString(parts[i].text)
			i++
			continue
		}
		end, ok := matchBarrierPhrase(parts, i)
		if !ok {
			b.WriteString(parts[i].text)
			i++
			continue
		}
		b.WriteString(matchVoiceCase(joinVoiceWords(parts, i, end), barrierPhraseReplacement))
		changed = true
		i = end + 1
	}
	if !changed {
		return s
	}
	return b.String()
}

func matchBarrierPhrase(parts []voiceChunk, i int) (int, bool) {
	if !voiceWordIs(parts[i], "hàng") {
		return 0, false
	}
	rao := nextVoiceWord(parts, i)
	if rao < 0 || !voiceWordIs(parts[rao], "rào") || !whitespaceOnlyBetween(parts, i, rao) {
		return 0, false
	}
	end := rao
	next := nextVoiceWord(parts, rao)
	if next >= 0 && whitespaceOnlyBetween(parts, rao, next) {
		if voiceWordIs(parts[next], "da") {
			end = next
		} else if voiceWordIs(parts[next], "bảo") {
			ve := nextVoiceWord(parts, next)
			if ve >= 0 && voiceWordIs(parts[ve], "vệ") && whitespaceOnlyBetween(parts, next, ve) {
				end = ve
			}
		}
	}
	for {
		extra := nextVoiceWord(parts, end)
		if extra < 0 || !voiceWordIs(parts[extra], "da") || !whitespaceOnlyBetween(parts, end, extra) {
			break
		}
		end = extra
	}
	return end, true
}

func nextVoiceWord(parts []voiceChunk, from int) int {
	for i := from + 1; i < len(parts); i++ {
		if parts[i].word {
			return i
		}
	}
	return -1
}

func voiceWordIs(part voiceChunk, want string) bool {
	return strings.EqualFold(norm.NFC.String(part.text), want)
}

func joinVoiceWords(parts []voiceChunk, from, to int) string {
	var b strings.Builder
	for i := from; i <= to && i < len(parts); i++ {
		if !parts[i].word {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(parts[i].text)
	}
	return b.String()
}

func rewriteZoneBarrier(in []dto.CoachZoneNote) []dto.CoachZoneNote {
	if len(in) == 0 {
		return nil
	}
	out := make([]dto.CoachZoneNote, len(in))
	for i, n := range in {
		n.Note = rewriteBarrierWording(n.Note)
		out[i] = n
	}
	return out
}

func rewriteScoreBarrier(in *dto.SkinCoachScoreNotes) *dto.SkinCoachScoreNotes {
	if in == nil {
		return nil
	}
	out := *in
	out.Overall = rewriteBarrierWording(in.Overall)
	out.Hydration = rewriteBarrierWording(in.Hydration)
	out.Clarity = rewriteBarrierWording(in.Clarity)
	out.Barrier = rewriteBarrierWording(in.Barrier)
	return &out
}

func rewriteQuestionBarrier(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, q := range in {
		out[i] = rewriteBarrierWording(q)
	}
	return out
}

func sanitizeQuestionList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, q := range in {
		q = strings.TrimSpace(SanitizeAndroidVoiceText(q))
		if q == "" {
			continue
		}
		out = append(out, q)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
