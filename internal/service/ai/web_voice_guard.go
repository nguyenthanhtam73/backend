package ai

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// webProfanityDrop are vulgar tokens removed from web coach copy.
// tao/mày stay. The Android sanitizer is a separate path and is not used here.
var webProfanityDrop = map[string]struct{}{
	"đm": {}, "dm": {}, "địt": {}, "đụ": {}, "đéo": {}, "đếch": {},
	"vl": {}, "vcl": {}, "vkl": {}, "vãi": {}, "lồn": {}, "đệt": {}, "cc": {},
}

// SanitizeWebCoachOutput drops swearing and technical numbers from web coach
// text. It keeps tao/mày. Android checks do not use this function.
func SanitizeWebCoachOutput(out *CoachStructuredOutput) bool {
	if out == nil {
		return false
	}
	changed := false
	out.SituationAnalysis = cleanWebField(out.SituationAnalysis, &changed)
	out.ConcernAlignment = cleanWebField(out.ConcernAlignment, &changed)
	out.SummaryNotes = cleanWebField(out.SummaryNotes, &changed)
	out.MedicalDisclaimer = cleanWebField(out.MedicalDisclaimer, &changed)
	out.Strengths = cleanWebList(out.Strengths, &changed)
	out.RoutineHints = cleanWebList(out.RoutineHints, &changed)
	out.AvoidOrPatch = cleanWebList(out.AvoidOrPatch, &changed)
	out.SafetyReminders = cleanWebList(out.SafetyReminders, &changed)
	for i := range out.Improvements {
		out.Improvements[i].Tip = cleanWebField(out.Improvements[i].Tip, &changed)
		out.Improvements[i].Why = cleanWebField(out.Improvements[i].Why, &changed)
	}
	for i := range out.CareSuggestions {
		out.CareSuggestions[i].Step = cleanWebField(out.CareSuggestions[i].Step, &changed)
		out.CareSuggestions[i].Why = cleanWebField(out.CareSuggestions[i].Why, &changed)
		out.CareSuggestions[i].SafetyNote = cleanWebField(out.CareSuggestions[i].SafetyNote, &changed)
	}
	return changed
}

// SanitizeWebCoachText cleans one web sentence.
func SanitizeWebCoachText(s string) string {
	next, _ := replaceWebVoiceTokens(s)
	return next
}

func cleanWebField(s string, changed *bool) string {
	next, ok := replaceWebVoiceTokens(s)
	if ok {
		*changed = true
	}
	return next
}

func cleanWebList(items []string, changed *bool) []string {
	if len(items) == 0 {
		return items
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		next := cleanWebField(item, changed)
		if strings.TrimSpace(next) == "" && strings.TrimSpace(item) != "" {
			*changed = true
			continue
		}
		out = append(out, next)
	}
	return out
}

func replaceWebVoiceTokens(s string) (string, bool) {
	if strings.TrimSpace(s) == "" {
		return s, false
	}
	parts := splitVoiceChunks(s)
	changed := false
	droppedPrefix := false
	kept := false
	var b strings.Builder
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if !part.word {
			if !kept && droppedPrefix && webLeadingPunct(part.text) {
				changed = true
				continue
			}
			b.WriteString(part.text)
			continue
		}
		key := strings.ToLower(part.text)
		if _, drop := webProfanityDrop[key]; drop || key == "score" || key == "scores" {
			changed = true
			if !kept {
				droppedPrefix = true
			}
			continue
		}
		if key == "0" && i+2 < len(parts) && webGaugeSep(parts[i+1].text) && webDigits(parts[i+2].text) && !webPercentAfter(parts, i+2) {
			changed = true
			if !kept {
				droppedPrefix = true
			}
			i += 2
			continue
		}
		if !kept && droppedPrefix && strings.EqualFold(key, "vì") {
			changed = true
			continue
		}
		text := part.text
		if !kept && droppedPrefix {
			text = capitalizeSentenceStart(text)
		}
		b.WriteString(text)
		kept = true
	}
	if !changed {
		return s, false
	}
	out := polishVoiceSpacing(b.String())
	out = strings.TrimRight(out, " ,;:")
	out = strings.TrimSpace(out)
	if !voiceHasLetterOrDigit(out) {
		return "", true
	}
	return out, true
}

func webLeadingPunct(s string) bool {
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

func webGaugeSep(s string) bool {
	return s == "." || s == ","
}

func webDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func webPercentAfter(parts []voiceChunk, idx int) bool {
	for j := idx + 1; j < len(parts); j++ {
		text := strings.TrimSpace(parts[j].text)
		if text == "" {
			continue
		}
		r, _ := utf8.DecodeRuneInString(text)
		if r == '%' {
			return true
		}
		if !parts[j].word && unicode.IsSpace(r) {
			continue
		}
		return false
	}
	return false
}
