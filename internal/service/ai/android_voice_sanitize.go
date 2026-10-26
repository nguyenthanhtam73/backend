package ai

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/dto"
	"golang.org/x/text/unicode/norm"
)

// androidVoiceDrop are standalone profanity tokens removed from Play-store copy.
// "đéo" and "đếch" are rewritten to "không" instead of deleted: dropping them
// flips the meaning ("đéo ổn" would become "ổn", "đéo được nặn" would become
// "được nặn"). "vãi là" is one exclamation — when "là" follows "vãi" with only
// whitespace between, both words are removed.
// androidVoiceSwap rewrites crude address. Match is case-insensitive.
// "mày" stays when it means eyebrow — see eyebrowMayPrev and eyebrowMayNext.
var (
	androidVoiceDrop = map[string]struct{}{
		"đm":  {},
		"dm":  {},
		"vl":  {},
		"vcl": {},
		"vkl": {},
		"địt": {},
		"đệt": {},
		"vãi": {},
	}
	androidVoiceSwap = map[string]string{
		"tao":  "mình",
		"mày":  "bạn",
		"đéo":  "không",
		"đếch": "không",
	}
	// eyebrowMayPrev is the word before "mày" when "mày" is an eyebrow, not "you".
	// "2" covers "giữa 2 mày"; "vùng" covers "Vùng mày".
	// "phần", "trên", "dưới", "quanh", "bên" cover phrases like "Phần mày trái".
	// "và" is intentionally absent: "và mày" is still the pronoun.
	eyebrowMayPrev = map[string]struct{}{
		"kẻ": {}, "tỉa": {}, "chì": {}, "đầu": {}, "đuôi": {}, "cung": {},
		"hai": {}, "giữa": {}, "phun": {}, "xăm": {}, "vẽ": {}, "sợi": {},
		"lông": {}, "chân": {}, "vùng": {}, "2": {},
		"phần": {}, "trên": {}, "dưới": {}, "quanh": {}, "bên": {},
	}
	// eyebrowMayNext is the word after "mày" when "mày" is an eyebrow.
	eyebrowMayNext = map[string]struct{}{
		"râu": {}, "mắt": {}, "ngài": {},
	}
)

// SanitizeAndroidCoachOutput rewrites obvious crude tokens in user-facing coach
// fields. Call it only for the android voice. Returns whether any field changed.
func SanitizeAndroidCoachOutput(out *CoachStructuredOutput) bool {
	if out == nil {
		return false
	}
	changed := false
	out.SituationAnalysis = sanitizeField(out.SituationAnalysis, &changed)
	out.ConcernAlignment = sanitizeField(out.ConcernAlignment, &changed)
	out.SummaryNotes = sanitizeField(out.SummaryNotes, &changed)
	out.MedicalDisclaimer = sanitizeField(out.MedicalDisclaimer, &changed)
	out.Strengths = sanitizeList(out.Strengths, &changed)
	out.RoutineHints = sanitizeList(out.RoutineHints, &changed)
	out.AvoidOrPatch = sanitizeList(out.AvoidOrPatch, &changed)
	out.SafetyReminders = sanitizeList(out.SafetyReminders, &changed)
	for i := range out.Improvements {
		out.Improvements[i].Tip = sanitizeField(out.Improvements[i].Tip, &changed)
		out.Improvements[i].Why = sanitizeField(out.Improvements[i].Why, &changed)
	}
	for i := range out.CareSuggestions {
		out.CareSuggestions[i].Step = sanitizeField(out.CareSuggestions[i].Step, &changed)
		out.CareSuggestions[i].Why = sanitizeField(out.CareSuggestions[i].Why, &changed)
		out.CareSuggestions[i].SafetyNote = sanitizeField(out.CareSuggestions[i].SafetyNote, &changed)
	}
	for i := range out.ProductSuggestions {
		out.ProductSuggestions[i] = sanitizeProductSuggestion(out.ProductSuggestions[i], &changed)
	}
	for i := range out.ProductGuidance {
		out.ProductGuidance[i] = sanitizeProductGuidance(out.ProductGuidance[i], &changed)
	}
	return changed
}

// SanitizeAndroidVoiceText rewrites one user-facing string. Empty input is unchanged.
func SanitizeAndroidVoiceText(s string) string {
	next, _ := replaceAndroidVoiceTokens(s)
	return next
}

func sanitizeField(s string, changed *bool) string {
	next, ok := replaceAndroidVoiceTokens(s)
	if ok {
		*changed = true
	}
	return next
}

func sanitizeList(items []string, changed *bool) []string {
	if len(items) == 0 {
		return items
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		next := sanitizeField(item, changed)
		// "• đm" filters down to a stray bullet. Drop it when no letter or
		// digit is left. An item that was already only punctuation stays.
		if !voiceHasLetterOrDigit(next) {
			if voiceHasLetterOrDigit(item) {
				*changed = true
				continue
			}
			out = append(out, next)
			continue
		}
		out = append(out, next)
	}
	return out
}

func sanitizeProductSuggestion(item dto.ProductSuggestion, changed *bool) dto.ProductSuggestion {
	item.Reason = sanitizeField(item.Reason, changed)
	item.Step = sanitizeField(item.Step, changed)
	item.HowToUse = sanitizeField(item.HowToUse, changed)
	item.Caution = sanitizeField(item.Caution, changed)
	item.Benefits = sanitizeList(item.Benefits, changed)
	return item
}

func sanitizeProductGuidance(item dto.ProductGuidanceItem, changed *bool) dto.ProductGuidanceItem {
	// NameOrCategory is a product name ("Kem Tao Skin", "Serum VL").
	// Rewriting it would turn the name into a different product.
	item.Why = sanitizeField(item.Why, changed)
	item.HowToUse = sanitizeField(item.HowToUse, changed)
	item.Caution = sanitizeField(item.Caution, changed)
	item.Benefits = sanitizeList(item.Benefits, changed)
	return item
}

type voiceChunk struct {
	word bool
	text string
}

// voiceEmit tracks words already written so a swap or deletion can collapse
// the duplicate it just created ("không đéo được" → "không được") without
// touching reduplication the sanitizer did not produce ("từ từ").
type voiceEmit struct {
	key         string
	has         bool
	wasSwap     bool
	collapseRun bool
	pendingDrop bool
	gapOK       bool
}

func replaceAndroidVoiceTokens(s string) (string, bool) {
	if strings.TrimSpace(s) == "" {
		return s, false
	}
	original := s
	// NFC folds a decomposed "đéo" or "mày" (base letter plus a combining
	// mark) into the composed tokens the drop and swap maps use.
	normalized := norm.NFC.String(s)
	lines := strings.Split(normalized, "\n")
	changed := false
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		next, lineChanged := replaceAndroidVoiceLine(line)
		if lineChanged {
			changed = true
			// "• đm vl" filters down to "•". A line with no letter or digit
			// left is empty, same as a profanity-only line.
			if !voiceHasLetterOrDigit(next) {
				continue
			}
		}
		kept = append(kept, next)
	}
	if !changed {
		return original, false
	}
	return strings.TrimSpace(strings.Join(kept, "\n")), true
}

func replaceAndroidVoiceLine(s string) (string, bool) {
	if strings.TrimSpace(s) == "" {
		return s, false
	}
	parts := splitVoiceChunks(s)
	nextWord, _ := voiceNextWords(parts)
	protected := urlOrPathTokenRanges(s)
	willDrop := voiceWordsToDrop(parts, protected)
	changed := false
	prevWord := ""
	sentenceStart := true
	droppedAtStart := false
	var emitted voiceEmit
	var b strings.Builder
	offset := 0
	for i, part := range parts {
		partStart := offset
		offset += len(part.text)
		if !part.word {
			// Punctuation left in front after a removed opener ("ĐM, mày" → "Bạn").
			if sentenceStart && droppedAtStart {
				continue
			}
			if repl, handled := voiceLeftoverPunct(parts, i, willDrop); handled {
				// A comma removed with the swear still separates words, so it
				// must not look like the sanitizer glued them together.
				if emitted.has && !horizontalSpaceOnly(part.text) {
					emitted.gapOK = false
				}
				if repl != "" {
					b.WriteString(repl)
					if chunkEndsSentence(repl) {
						sentenceStart = true
						droppedAtStart = false
					}
				}
				continue
			}
			if emitted.has && !horizontalSpaceOnly(part.text) {
				emitted.gapOK = false
			}
			b.WriteString(part.text)
			if chunkEndsSentence(part.text) {
				sentenceStart = true
				droppedAtStart = false
			}
			continue
		}
		key := strings.ToLower(part.text)
		// URLs, paths, and codes like "DM-2024" are not coach prose.
		keepRaw := spanCovers(protected, partStart, offset) || hyphenJoinedToDigits(parts, i)
		if willDrop[i] {
			changed = true
			prevWord = key
			emitted.pendingDrop = true
			if sentenceStart {
				droppedAtStart = true
			}
			continue
		}
		text := part.text
		swapped := false
		if !keepRaw {
			if repl, ok := androidVoiceSwap[key]; ok && !keepEyebrowMay(key, prevWord, nextWord[i]) {
				changed = true
				swapped = true
				text = matchVoiceCase(part.text, repl)
			}
		}
		if !keepRaw && sentenceStart && droppedAtStart {
			text = capitalizeSentenceStart(text)
		}
		if emitted.collapse(strings.ToLower(text), swapped) {
			changed = true
			prevWord = key
			sentenceStart = false
			droppedAtStart = false
			continue
		}
		b.WriteString(text)
		emitted.note(strings.ToLower(text), swapped)
		prevWord = key
		sentenceStart = false
		droppedAtStart = false
	}
	if !changed {
		return s, false
	}
	return polishVoiceSpacing(b.String()), true
}

// collapse reports whether this output word repeats the previous one only
// because a swap or a dropped token made them neighbors. Punctuation between
// the two words keeps both.
func (e *voiceEmit) collapse(outKey string, swapped bool) bool {
	if !e.has || outKey == "" || outKey != e.key || !e.gapOK {
		return false
	}
	if !swapped && !e.wasSwap && !e.pendingDrop && !e.collapseRun {
		return false
	}
	e.wasSwap = e.wasSwap || swapped
	e.collapseRun = true
	e.pendingDrop = false
	e.gapOK = true
	return true
}

func (e *voiceEmit) note(outKey string, swapped bool) {
	e.key = outKey
	e.has = true
	e.wasSwap = swapped
	e.collapseRun = false
	e.pendingDrop = false
	e.gapOK = true
}

func voiceNextWords(parts []voiceChunk) ([]string, []int) {
	next := make([]string, len(parts))
	at := make([]int, len(parts))
	for i := range at {
		at[i] = -1
	}
	upcoming := ""
	upcomingAt := -1
	for i := len(parts) - 1; i >= 0; i-- {
		next[i] = upcoming
		at[i] = upcomingAt
		if parts[i].word {
			upcoming = strings.ToLower(parts[i].text)
			upcomingAt = i
		}
	}
	return next, at
}

func horizontalSpaceOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

func whitespaceOnlyBetween(parts []voiceChunk, from, to int) bool {
	for k := from + 1; k < to; k++ {
		if parts[k].word {
			return false
		}
		for _, r := range parts[k].text {
			if !unicode.IsSpace(r) {
				return false
			}
		}
	}
	return true
}

func keepEyebrowMay(word, prev, next string) bool {
	if word != "mày" {
		return false
	}
	if _, ok := eyebrowMayPrev[prev]; ok {
		return true
	}
	_, ok := eyebrowMayNext[next]
	return ok
}

func chunkEndsSentence(s string) bool {
	for _, r := range s {
		switch r {
		case '.', '!', '?', '…':
			return true
		}
	}
	return false
}

func capitalizeSentenceStart(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError || size == 0 || !unicode.IsLetter(r) || unicode.IsUpper(r) {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

func splitVoiceChunks(s string) []voiceChunk {
	var out []voiceChunk
	var b strings.Builder
	word := false
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, voiceChunk{word: word, text: b.String()})
		b.Reset()
	}
	for _, r := range s {
		isWord := unicode.IsLetter(r) || unicode.IsDigit(r)
		if b.Len() > 0 && isWord != word {
			flush()
		}
		word = isWord
		b.WriteRune(r)
	}
	flush()
	return out
}

func matchVoiceCase(original, replacement string) string {
	if voiceAllCaps(original) {
		return strings.ToUpper(replacement)
	}
	r, _ := utf8.DecodeRuneInString(original)
	if r == utf8.RuneError || !unicode.IsUpper(r) {
		return replacement
	}
	rr, size := utf8.DecodeRuneInString(replacement)
	if rr == utf8.RuneError || size == 0 {
		return replacement
	}
	return string(unicode.ToUpper(rr)) + replacement[size:]
}

func voiceAllCaps(s string) bool {
	letters := false
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		letters = true
		if !unicode.IsUpper(r) {
			return false
		}
	}
	return letters
}

func voiceHasLetterOrDigit(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

type byteSpan struct {
	start int
	end   int
}

// urlOrPathTokenRanges marks whitespace-delimited tokens that contain "://"
// or "/". Words inside them are left alone ("dadiary.vn/dm/x?vl=1").
func urlOrPathTokenRanges(s string) []byteSpan {
	var spans []byteSpan
	tokenStart := 0
	inToken := false
	flush := func(end int) {
		if !inToken {
			return
		}
		token := s[tokenStart:end]
		if strings.Contains(token, "://") || strings.Contains(token, "/") {
			spans = append(spans, byteSpan{start: tokenStart, end: end})
		}
		inToken = false
	}
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			flush(i)
			i += size
			continue
		}
		if !inToken {
			tokenStart = i
			inToken = true
		}
		i += size
	}
	flush(len(s))
	return spans
}

func spanCovers(spans []byteSpan, start, end int) bool {
	for _, sp := range spans {
		if start >= sp.start && end <= sp.end {
			return true
		}
	}
	return false
}

// hyphenJoinedToDigits reports a word glued to a digit token by "-", as in
// "DM-2024". Spaces around the hyphen do not count.
func hyphenJoinedToDigits(parts []voiceChunk, i int) bool {
	if i < 0 || i >= len(parts) || !parts[i].word {
		return false
	}
	if i >= 2 && parts[i-2].word && hyphenOnly(parts[i-1].text) && containsDigit(parts[i-2].text) {
		return true
	}
	if i+2 < len(parts) && parts[i+2].word && hyphenOnly(parts[i+1].text) && containsDigit(parts[i+2].text) {
		return true
	}
	return false
}

func hyphenOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '-' {
			return false
		}
	}
	return true
}

func containsDigit(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// voiceWordsToDrop marks profanity tokens that are deleted rather than
// rewritten. Protected URL and code words stay, matching the main loop.
func voiceWordsToDrop(parts []voiceChunk, protected []byteSpan) []bool {
	drop := make([]bool, len(parts))
	if len(parts) == 0 {
		return drop
	}
	starts := make([]int, len(parts))
	offset := 0
	for i, part := range parts {
		starts[i] = offset
		offset += len(part.text)
	}
	keepRaw := func(i int) bool {
		end := starts[i] + len(parts[i].text)
		return spanCovers(protected, starts[i], end) || hyphenJoinedToDigits(parts, i)
	}
	nextWord, nextAt := voiceNextWords(parts)
	for i, part := range parts {
		if !part.word || keepRaw(i) {
			continue
		}
		key := strings.ToLower(part.text)
		if _, ok := androidVoiceDrop[key]; ok {
			drop[i] = true
		}
		if key == "vãi" && nextWord[i] == "là" && nextAt[i] >= 0 && whitespaceOnlyBetween(parts, i, nextAt[i]) && !keepRaw(nextAt[i]) {
			drop[i] = true
			drop[nextAt[i]] = true
		}
	}
	return drop
}

// voiceLeftoverPunct rewrites a comma that only remains because a word was
// removed: "nốt nhỏ, đm." → "nốt nhỏ.", "da, đm, khá khô" → "da, khá khô".
// A comma between digits ("1,5") or in text the sanitizer did not edit is
// left alone. handled is false when this chunk should be copied through.
func voiceLeftoverPunct(parts []voiceChunk, i int, willDrop []bool) (string, bool) {
	text := parts[i].text
	if !chunkHasComma(text) || !commaRunTouchesDrop(parts, i, willDrop) {
		return "", false
	}
	if end, ok := commaThenEndPunct(text); ok {
		return end, true
	}
	// "," and the following space are one chunk ("nốt nhỏ, đm.").
	if !commaSpaceOnly(text) {
		return "", false
	}
	_, right := expandCommaRun(parts, i, willDrop)
	if commaRunKeepsOne(parts, right, willDrop) && lastCommaInRun(parts, i, right) {
		return keptCommaText(text), true
	}
	// Either an earlier comma in this run is the one we keep, or the
	// comma sits at the end of the line / in front of . ! ?
	return "", true
}

func chunkHasComma(s string) bool {
	return strings.Contains(s, ",")
}

func commaSpaceOnly(s string) bool {
	if !chunkHasComma(s) {
		return false
	}
	for _, r := range s {
		if r != ',' && r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

func keptCommaText(chunk string) string {
	if strings.ContainsAny(chunk, " \t") {
		return ", "
	}
	return ","
}

// commaThenEndPunct accepts a chunk that is only commas, spaces, and one
// sentence mark, such as ",." or ", !".
func commaThenEndPunct(s string) (string, bool) {
	if !chunkHasComma(s) {
		return "", false
	}
	end := rune(0)
	for _, r := range s {
		switch r {
		case ',', ' ', '\t':
		case '.', '!', '?', '…':
			end = r
		default:
			return "", false
		}
	}
	if end == 0 {
		return "", false
	}
	return string(end), true
}

func expandCommaRun(parts []voiceChunk, i int, willDrop []bool) (left, right int) {
	left, right = i, i
	for left > 0 {
		j := left - 1
		if commaRunStep(parts, j, willDrop) {
			left = j
			continue
		}
		break
	}
	for right+1 < len(parts) {
		j := right + 1
		if commaRunStep(parts, j, willDrop) {
			right = j
			continue
		}
		break
	}
	return left, right
}

func commaRunStep(parts []voiceChunk, j int, willDrop []bool) bool {
	if parts[j].word {
		return willDrop[j]
	}
	return horizontalSpaceOnly(parts[j].text) || commaSpaceOnly(parts[j].text)
}

func commaRunTouchesDrop(parts []voiceChunk, i int, willDrop []bool) bool {
	left, right := expandCommaRun(parts, i, willDrop)
	for j := left; j <= right; j++ {
		if parts[j].word && willDrop[j] {
			return true
		}
	}
	for j := left - 1; j >= 0; j-- {
		if horizontalSpaceOnly(parts[j].text) {
			continue
		}
		return parts[j].word && willDrop[j]
	}
	return false
}

// commaRunKeepsOne is true when a real word still follows the comma run, so
// one comma remains ("da, đm, khá" → "da, khá"). End punctuation and the
// end of the line drop the comma instead.
func commaRunKeepsOne(parts []voiceChunk, right int, willDrop []bool) bool {
	for j := right + 1; j < len(parts); j++ {
		if horizontalSpaceOnly(parts[j].text) {
			continue
		}
		return parts[j].word && !willDrop[j]
	}
	return false
}

func lastCommaInRun(parts []voiceChunk, i, right int) bool {
	for j := i + 1; j <= right; j++ {
		if commaSpaceOnly(parts[j].text) {
			return false
		}
	}
	return true
}

func polishVoiceSpacing(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if space {
				continue
			}
			space = true
			b.WriteRune(' ')
			continue
		}
		space = false
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	out = strings.ReplaceAll(out, " ,", ",")
	out = strings.ReplaceAll(out, " .", ".")
	out = strings.ReplaceAll(out, " !", "!")
	out = strings.ReplaceAll(out, " ?", "?")
	out = strings.ReplaceAll(out, " ;", ";")
	out = strings.ReplaceAll(out, " :", ":")
	return out
}
