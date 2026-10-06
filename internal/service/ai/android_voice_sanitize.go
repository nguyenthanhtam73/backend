package ai

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/dto"
)

// androidVoiceDrop are standalone profanity tokens removed from Play-store copy.
// androidVoiceSwap rewrites crude address to the polite pair. Match is case-insensitive.
// "lông mày" and "chân mày" keep mày (eyebrow), not the pronoun.
var (
	androidVoiceDrop = map[string]struct{}{
		"đm":  {},
		"đéo": {},
		"vl":  {},
		"vcl": {},
	}
	androidVoiceSwap = map[string]string{
		"tao": "mình",
		"mày": "bạn",
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
	changed := false
	prevWord := ""
	var b strings.Builder
	for _, part := range parts {
		if !part.word {
			b.WriteString(part.text)
			continue
		}
		key := strings.ToLower(part.text)
		if _, drop := androidVoiceDrop[key]; drop {
			changed = true
			prevWord = key
			continue
		}
		if repl, ok := androidVoiceSwap[key]; ok && !keepEyebrowMay(key, prevWord) {
			changed = true
			b.WriteString(matchVoiceCase(part.text, repl))
			prevWord = key
			continue
		}
		b.WriteString(part.text)
		prevWord = key
	}
	if !changed {
		return s, false
	}
	return polishVoiceSpacing(b.String()), true
}

func keepEyebrowMay(word, prev string) bool {
	if word != "mày" {
		return false
	}
	return prev == "lông" || prev == "chân"
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
