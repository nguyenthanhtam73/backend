package ai

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/dto"
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
	eyebrowMayPrev = map[string]struct{}{
		"kẻ": {}, "tỉa": {}, "chì": {}, "đầu": {}, "đuôi": {}, "cung": {},
		"hai": {}, "giữa": {}, "phun": {}, "xăm": {}, "vẽ": {}, "sợi": {},
		"lông": {}, "chân": {},
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
	for i, item := range items {
		items[i] = sanitizeField(item, changed)
	}
	return items
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
	item.NameOrCategory = sanitizeField(item.NameOrCategory, changed)
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

func replaceAndroidVoiceTokens(s string) (string, bool) {
	if strings.TrimSpace(s) == "" {
		return s, false
	}
	parts := splitVoiceChunks(s)
	nextWord, nextWordAt := voiceNextWords(parts)
	dropAlso := map[int]bool{}
	changed := false
	prevWord := ""
	sentenceStart := true
	droppedAtStart := false
	var b strings.Builder
	for i, part := range parts {
		if !part.word {
			// Punctuation left in front after a removed opener ("ĐM, mày" → "Bạn").
			if sentenceStart && droppedAtStart {
				continue
			}
			b.WriteString(part.text)
			if chunkEndsSentence(part.text) {
				sentenceStart = true
				droppedAtStart = false
			}
			continue
		}
		key := strings.ToLower(part.text)
		dropPhrase := key == "vãi" && nextWord[i] == "là" && nextWordAt[i] >= 0 && whitespaceOnlyBetween(parts, i, nextWordAt[i])
		if _, drop := androidVoiceDrop[key]; drop || dropAlso[i] || dropPhrase {
			if dropPhrase {
				dropAlso[nextWordAt[i]] = true
			}
			changed = true
			prevWord = key
			if sentenceStart {
				droppedAtStart = true
			}
			continue
		}
		text := part.text
		if repl, ok := androidVoiceSwap[key]; ok && !keepEyebrowMay(key, prevWord, nextWord[i]) {
			changed = true
			text = matchVoiceCase(part.text, repl)
		}
		if sentenceStart && droppedAtStart {
			text = capitalizeSentenceStart(text)
		}
		b.WriteString(text)
		prevWord = key
		sentenceStart = false
		droppedAtStart = false
	}
	if !changed {
		return s, false
	}
	return polishVoiceSpacing(b.String()), true
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
