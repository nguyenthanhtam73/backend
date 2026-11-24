package ai

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/dto"
	"golang.org/x/text/unicode/norm"
)

func init() {
	// Read path lives in dto, which cannot import this package.
	dto.CleanPublicCoachText = SanitizePublicCopy
}

// scoreLeakPattern matches the standalone word score/scores, including the
// "score" inside a snake_case key such as skin_score_notes. It does not match
// a longer word like "scored".
var scoreLeakPattern = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])scores?(?:[^A-Za-z0-9]|$)`)

var snakeFieldPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:_[A-Za-z0-9]+)+$`)

// SanitizePublicCopy strips technical tokens from one sentence a person reads.
// It removes the word score, gauge decimals (0.78 / 0,78), and snake_case
// field names. "score 0.78 vì da vẫn khỏe mạnh" becomes "Da vẫn khỏe mạnh".
// Counts, SPF50, dates, "ngày 7", and percentages stay. Pronouns and
// profanity are left for the Android voice filter.
func SanitizePublicCopy(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	toks := tokenizePublicCopy(s)
	drop := decimalLeakFlags(toks)
	var b strings.Builder
	sentenceStart := true
	droppedAtSentence := false
	changed := false
	for i, tok := range toks {
		if drop[i] || tok.kind == "snake" || tok.kind == "score" {
			changed = true
			if sentenceStart {
				droppedAtSentence = true
			}
			continue
		}
		switch tok.kind {
		case "space":
			b.WriteString(tok.text)
		case "punct":
			if sentenceStart && droppedAtSentence && leadingConnectorPunct(tok.text) {
				changed = true
				continue
			}
			b.WriteString(tok.text)
			if chunkEndsSentence(tok.text) {
				sentenceStart = true
				droppedAtSentence = false
			}
		default:
			if sentenceStart && droppedAtSentence && isViConnector(tok.text) {
				changed = true
				continue
			}
			text := tok.text
			if sentenceStart && droppedAtSentence {
				text = capitalizeSentenceStart(text)
			}
			b.WriteString(text)
			sentenceStart = false
			droppedAtSentence = false
		}
	}
	if !changed {
		return s
	}
	return finishPublicCopy(b.String())
}

// SanitizePublicCoachOutput applies SanitizePublicCopy to every user-facing
// coach field, including zone notes and gauge reasons. It does not rewrite
// pronouns. Call it after parse so a new row is stored clean, and rely on
// the read path for rows already saved.
func SanitizePublicCoachOutput(out *CoachStructuredOutput) bool {
	if out == nil {
		return false
	}
	changed := false
	out.SituationAnalysis = cleanPublicField(out.SituationAnalysis, &changed)
	out.ConcernAlignment = cleanPublicField(out.ConcernAlignment, &changed)
	out.SummaryNotes = cleanPublicField(out.SummaryNotes, &changed)
	out.MedicalDisclaimer = cleanPublicField(out.MedicalDisclaimer, &changed)
	out.Strengths = cleanPublicList(out.Strengths, &changed)
	out.RoutineHints = cleanPublicList(out.RoutineHints, &changed)
	out.AvoidOrPatch = cleanPublicList(out.AvoidOrPatch, &changed)
	out.SafetyReminders = cleanPublicList(out.SafetyReminders, &changed)
	for i := range out.Improvements {
		out.Improvements[i].Tip = cleanPublicField(out.Improvements[i].Tip, &changed)
		out.Improvements[i].Why = cleanPublicField(out.Improvements[i].Why, &changed)
	}
	for i := range out.CareSuggestions {
		out.CareSuggestions[i].Step = cleanPublicField(out.CareSuggestions[i].Step, &changed)
		out.CareSuggestions[i].Why = cleanPublicField(out.CareSuggestions[i].Why, &changed)
		out.CareSuggestions[i].SafetyNote = cleanPublicField(out.CareSuggestions[i].SafetyNote, &changed)
	}
	for i := range out.ProductSuggestions {
		item := &out.ProductSuggestions[i]
		item.Reason = cleanPublicField(item.Reason, &changed)
		item.Step = cleanPublicField(item.Step, &changed)
		item.HowToUse = cleanPublicField(item.HowToUse, &changed)
		item.Caution = cleanPublicField(item.Caution, &changed)
		item.Benefits = cleanPublicList(item.Benefits, &changed)
	}
	for i := range out.ProductGuidance {
		item := &out.ProductGuidance[i]
		item.Why = cleanPublicField(item.Why, &changed)
		item.HowToUse = cleanPublicField(item.HowToUse, &changed)
		item.Caution = cleanPublicField(item.Caution, &changed)
		item.Benefits = cleanPublicList(item.Benefits, &changed)
	}
	out.ZoneNotesRaw = cleanZoneNotesRaw(out.ZoneNotesRaw, &changed)
	out.SkinScoreNotesRaw = cleanScoreNotesRaw(out.SkinScoreNotesRaw, &changed)
	return changed
}

// CoachProseHasScoreLeak reports whether user-facing text contains the word
// score or a gauge decimal the filter would remove. Doses with a unit, ranges,
// and ingredient strengths do not match.
func CoachProseHasScoreLeak(s string) bool {
	if scoreLeakPattern.MatchString(s) {
		return true
	}
	if strings.TrimSpace(s) == "" {
		return false
	}
	toks := tokenizePublicCopy(s)
	for _, leak := range decimalLeakFlags(toks) {
		if leak {
			return true
		}
	}
	return false
}

// CoachProseHasTaoMay reports whether user-facing text contains the words
// tao or mày. The admin count uses this. The public filter does not.
func CoachProseHasTaoMay(s string) bool {
	parts := splitVoiceChunks(norm.NFC.String(s))
	next, _ := voiceNextWords(parts)
	prev := ""
	for i, part := range parts {
		if !part.word {
			continue
		}
		key := strings.ToLower(part.text)
		if key == "tao" {
			return true
		}
		// "lông mày" and "kẻ mày" are eyebrows, not the pronoun.
		if key == "mày" && !keepEyebrowMay(key, prev, next[i]) {
			return true
		}
		prev = key
	}
	return false
}

// UserFacingCoachCorpus joins summary_notes with the string values inside
// coach JSON columns. Numeric gauges and object keys are not included, so a
// stored 0.78 gauge is not itself a leak.
func UserFacingCoachCorpus(summary string, jsonBlobs ...string) string {
	var b strings.Builder
	b.WriteString(summary)
	for _, blob := range jsonBlobs {
		for _, s := range jsonStringValues(blob) {
			b.WriteByte('\n')
			b.WriteString(s)
		}
	}
	return b.String()
}

func cleanPublicField(s string, changed *bool) string {
	next := SanitizePublicCopy(s)
	if next != s {
		*changed = true
	}
	return next
}

func cleanPublicList(items []string, changed *bool) []string {
	if len(items) == 0 {
		return items
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		next := cleanPublicField(item, changed)
		if strings.TrimSpace(next) == "" {
			if strings.TrimSpace(item) != "" {
				*changed = true
			}
			continue
		}
		out = append(out, next)
	}
	return out
}

func cleanZoneNotesRaw(raw json.RawMessage, changed *bool) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return raw
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return raw
	}
	dirty := false
	for _, item := range items {
		note, ok := item["note"].(string)
		if !ok {
			continue
		}
		next := SanitizePublicCopy(note)
		if next != note {
			dirty = true
			item["note"] = next
		}
	}
	if !dirty {
		return raw
	}
	*changed = true
	b, err := json.Marshal(items)
	if err != nil {
		return raw
	}
	return b
}

func cleanScoreNotesRaw(raw json.RawMessage, changed *bool) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return raw
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	dirty := false
	for _, key := range []string{"overall", "hydration", "clarity", "barrier"} {
		note, ok := obj[key].(string)
		if !ok {
			continue
		}
		next := SanitizePublicCopy(note)
		if next != note {
			dirty = true
			obj[key] = next
		}
	}
	if !dirty {
		return raw
	}
	*changed = true
	b, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return b
}

type publicTok struct {
	kind string
	text string
}

func tokenizePublicCopy(s string) []publicTok {
	protected := urlOrPathTokenRanges(s)
	var out []publicTok
	i := 0
	for i < len(s) {
		if end, ok := protectedEnd(protected, i); ok {
			out = append(out, publicTok{kind: "word", text: s[i:end]})
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			j := i + size
			for j < len(s) {
				rr, sz := utf8.DecodeRuneInString(s[j:])
				if rr != ' ' && rr != '\t' && rr != '\n' && rr != '\r' {
					break
				}
				j += sz
			}
			out = append(out, publicTok{kind: "space", text: s[i:j]})
			i = j
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			j := i + size
			ascii := isASCIIAlnum(r) || r == '_'
			underscore := r == '_'
			for j < len(s) {
				rr, sz := utf8.DecodeRuneInString(s[j:])
				if rr != '_' && !unicode.IsLetter(rr) && !unicode.IsDigit(rr) {
					break
				}
				if rr == '_' {
					underscore = true
				}
				if rr != '_' && !isASCIIAlnum(rr) {
					ascii = false
				}
				j += sz
			}
			text := s[i:j]
			if ascii && underscore && snakeFieldPattern.MatchString(text) {
				out = append(out, publicTok{kind: "snake", text: text})
				i = j
				continue
			}
			if strings.EqualFold(text, "score") || strings.EqualFold(text, "scores") {
				out = append(out, publicTok{kind: "score", text: text})
				i = j
				continue
			}
			if text == "0" {
				if end, keep := readGauge(s, j); end > j {
					kind := "decimal"
					if keep {
						kind = "word"
					}
					out = append(out, publicTok{kind: kind, text: s[i:end]})
					i = end
					continue
				}
			}
			if j < len(s) && s[j] == '%' {
				j++
			}
			out = append(out, publicTok{kind: "word", text: s[i:j]})
			i = j
			continue
		}
		// One punctuation rune, or a run of identical sentence marks.
		j := i + size
		out = append(out, publicTok{kind: "punct", text: s[i:j]})
		i = j
	}
	return out
}

func protectedEnd(spans []byteSpan, i int) (int, bool) {
	for _, sp := range spans {
		if i >= sp.start && i < sp.end {
			return sp.end, true
		}
	}
	return 0, false
}

func isASCIIAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// readGauge consumes ".78" or ",78" after a leading 0.
// keep is true when the number is a dose or a percentage ("0.5g", "0,5ml",
// "0.5%"), which is not a gauge leak.
func readGauge(s string, zeroEnd int) (end int, keep bool) {
	if zeroEnd >= len(s) {
		return 0, false
	}
	sep := s[zeroEnd]
	if sep != '.' && sep != ',' {
		return 0, false
	}
	k := zeroEnd + 1
	digits := 0
	for k < len(s) && s[k] >= '0' && s[k] <= '9' {
		k++
		digits++
	}
	if digits == 0 {
		return 0, false
	}
	if unitEnd := gluedDoseUnit(s, k); unitEnd > k {
		return unitEnd, true
	}
	return k, false
}

func gluedDoseUnit(s string, k int) int {
	if k >= len(s) {
		return k
	}
	if s[k] == '%' {
		return k + 1
	}
	end := k
	for end < len(s) {
		r, sz := utf8.DecodeRuneInString(s[end:])
		if !unicode.IsLetter(r) {
			break
		}
		end += sz
	}
	if end == k || !isDoseUnit(s[k:end]) {
		return k
	}
	return end
}

func isDoseUnit(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ml", "g", "mg", "cm", "mm", "lít", "lit", "giọt", "%":
		return true
	default:
		return false
	}
}

// decimalLeakFlags marks 0.xx / 0,xx tokens that sit next to "score", a
// snake_case field name, or a "word:" label. Doses with a unit and ranges
// (0.5–2, 0.5 đến 2) are never marked.
func decimalLeakFlags(toks []publicTok) []bool {
	drop := make([]bool, len(toks))
	for i, tok := range toks {
		if tok.kind != "decimal" {
			continue
		}
		if decimalHasUnit(toks, i) || decimalInRange(toks, i) {
			continue
		}
		label, colon := labelBeforeDecimal(toks, i)
		if label >= 0 && isIngredientLabel(toks, label) {
			continue
		}
		if !decimalNextToLeakLabel(toks, i) {
			continue
		}
		drop[i] = true
		if label >= 0 && isGaugeKey(toks[label].text) {
			drop[label] = true
			if colon >= 0 {
				drop[colon] = true
			}
		}
	}
	return drop
}

func labelBeforeDecimal(toks []publicTok, i int) (label, colon int) {
	prev := prevContent(toks, i)
	if prev < 0 || !isColon(toks[prev].text) {
		return -1, -1
	}
	label = prevContent(toks, prev)
	if label < 0 || toks[label].kind == "punct" || toks[label].kind == "space" || toks[label].kind == "decimal" {
		return -1, -1
	}
	return label, prev
}

func isGaugeKey(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "overall", "hydration", "clarity", "barrier", "score", "scores":
		return true
	default:
		return false
	}
}

func isIngredientLabel(toks []publicTok, label int) bool {
	key := strings.ToLower(strings.TrimSpace(toks[label].text))
	if _, ok := ingredientNames[key]; ok {
		return true
	}
	if key == "c" {
		prev := prevContent(toks, label)
		return prev >= 0 && strings.EqualFold(strings.TrimSpace(toks[prev].text), "vitamin")
	}
	return false
}

var ingredientNames = map[string]struct{}{
	"retinol": {}, "retinoid": {}, "tretinoin": {}, "adapalene": {},
	"bha": {}, "aha": {}, "pha": {}, "niacinamide": {}, "ceramide": {},
	"ceramides": {}, "azelaic": {}, "benzoyl": {}, "salicylic": {},
	"vitamin": {},
}

func decimalHasUnit(toks []publicTok, i int) bool {
	next := nextContent(toks, i)
	if next < 0 {
		return false
	}
	if toks[next].kind == "punct" && strings.TrimSpace(toks[next].text) == "%" {
		return true
	}
	return toks[next].kind == "word" && isDoseUnit(toks[next].text)
}

func decimalInRange(toks []publicTok, i int) bool {
	if sideIsRange(toks, nextContent(toks, i), true) {
		return true
	}
	return sideIsRange(toks, prevContent(toks, i), false)
}

func sideIsRange(toks []publicTok, j int, forward bool) bool {
	if j < 0 {
		return false
	}
	text := strings.TrimSpace(toks[j].text)
	if text == "-" || text == "–" || text == "~" || text == "—" {
		return true
	}
	if strings.EqualFold(text, "đến") {
		return true
	}
	// "0.5–2" can arrive as one punctuation rune. Already covered.
	_ = forward
	return false
}

func decimalNextToLeakLabel(toks []publicTok, i int) bool {
	prev := prevContent(toks, i)
	if prev >= 0 {
		if toks[prev].kind == "score" || toks[prev].kind == "snake" {
			return true
		}
		if isColon(toks[prev].text) {
			label := prevContent(toks, prev)
			if label >= 0 && toks[label].kind != "punct" && toks[label].kind != "space" && toks[label].kind != "decimal" {
				return true
			}
		}
	}
	next := nextContent(toks, i)
	if next >= 0 && (toks[next].kind == "score" || toks[next].kind == "snake") {
		return true
	}
	return false
}

func isColon(s string) bool {
	return strings.TrimSpace(s) == ":"
}

func prevContent(toks []publicTok, i int) int {
	for j := i - 1; j >= 0; j-- {
		if toks[j].kind != "space" {
			return j
		}
	}
	return -1
}

func nextContent(toks []publicTok, i int) int {
	for j := i + 1; j < len(toks); j++ {
		if toks[j].kind != "space" {
			return j
		}
	}
	return -1
}

func leadingConnectorPunct(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch r {
		case ',', ':', ';', ' ', '\t':
		default:
			return false
		}
	}
	return true
}

func isViConnector(s string) bool {
	return strings.EqualFold(s, "vì")
}

func finishPublicCopy(s string) string {
	s = polishVoiceSpacing(s)
	s = strings.ReplaceAll(s, "()", "")
	s = strings.ReplaceAll(s, "( )", "")
	s = strings.ReplaceAll(s, ",.", ".")
	s = strings.ReplaceAll(s, ",!", "!")
	s = strings.ReplaceAll(s, ",?", "?")
	s = strings.ReplaceAll(s, ",…", "…")
	s = polishVoiceSpacing(s)
	s = strings.TrimRight(s, " ,;:")
	s = strings.TrimSpace(s)
	if !voiceHasLetterOrDigit(s) {
		return ""
	}
	return s
}

func jsonStringValues(blob string) []string {
	blob = strings.TrimSpace(blob)
	if blob == "" || blob == "null" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(blob), &v); err != nil {
		return []string{blob}
	}
	var out []string
	walkJSONStrings(v, &out)
	return out
}

func walkJSONStrings(v any, out *[]string) {
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) != "" {
			*out = append(*out, t)
		}
	case []any:
		for _, item := range t {
			walkJSONStrings(item, out)
		}
	case map[string]any:
		for _, item := range t {
			walkJSONStrings(item, out)
		}
	}
}
