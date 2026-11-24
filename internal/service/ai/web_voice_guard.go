package ai

import (
	"encoding/json"
	"strings"

	"github.com/dadiary/backend/internal/dto"
	"golang.org/x/text/unicode/norm"
)

func init() {
	// Read path for stored coach JSON. Technical-number stripping stays in #82.
	dto.CleanWebProfanity = SanitizeWebCoachText
}

// webProfanityExact matches accented swears with their diacritics kept.
// Case-insensitive and NFC only. tao/mày stay.
var webProfanityExact = map[string]struct{}{
	"đm": {}, "địt": {}, "đụ": {}, "đéo": {}, "đếch": {},
	"lồn": {}, "vãi": {}, "đệt": {},
}

// webProfanityPlain matches slang that is unaccented by nature.
// đcm and đmm fold only the letter đ onto dcm and dmm.
var webProfanityPlain = map[string]struct{}{
	"dm": {}, "vl": {}, "vcl": {}, "vkl": {}, "cc": {},
	"dcm": {}, "dmm": {}, "cmn": {}, "clgt": {},
}

// SanitizeWebCoachOutput drops swearing from web coach text. It keeps tao/mày
// and does not touch numbers. Android checks do not use this function.
func SanitizeWebCoachOutput(out *CoachStructuredOutput) bool {
	return sanitizeWebFull(out, false)
}

func sanitizeWebFull(out *CoachStructuredOutput, worseThanLast bool) bool {
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
	for i := range out.ProductSuggestions {
		item := &out.ProductSuggestions[i]
		item.Reason = cleanWebField(item.Reason, &changed)
		item.Step = cleanWebField(item.Step, &changed)
		item.HowToUse = cleanWebField(item.HowToUse, &changed)
		item.Caution = cleanWebField(item.Caution, &changed)
		item.Benefits = cleanWebList(item.Benefits, &changed)
	}
	for i := range out.ProductGuidance {
		item := &out.ProductGuidance[i]
		item.Why = cleanWebField(item.Why, &changed)
		item.Step = cleanWebField(item.Step, &changed)
		item.HowToUse = cleanWebField(item.HowToUse, &changed)
		item.Caution = cleanWebField(item.Caution, &changed)
		item.Benefits = cleanWebList(item.Benefits, &changed)
	}
	out.ZoneNotesRaw = cleanWebZoneNotes(out.ZoneNotesRaw, &changed)
	out.SkinScoreNotesRaw = cleanWebScoreNotes(out.SkinScoreNotesRaw, &changed)
	if sanitizeWebCoachOutput(out, worseThanLast) {
		changed = true
	}
	return changed
}

// SanitizeWebStarterRoutine drops swearing from every sentence of an onboarding
// starter routine, including product reasons and how-to text.
func SanitizeWebStarterRoutine(out *StarterRoutine) bool {
	if out == nil {
		return false
	}
	changed := false
	out.Morning = cleanWebList(out.Morning, &changed)
	out.Evening = cleanWebList(out.Evening, &changed)
	out.WeekNotes = cleanWebField(out.WeekNotes, &changed)
	out.SafetyNotes = cleanWebField(out.SafetyNotes, &changed)
	out.Encouragement = cleanWebField(out.Encouragement, &changed)
	out.SkinReadback = cleanWebField(out.SkinReadback, &changed)
	out.Rationale = cleanWebField(out.Rationale, &changed)
	out.ClosingReminder = cleanWebField(out.ClosingReminder, &changed)
	for i := range out.ProductSuggestions {
		item := &out.ProductSuggestions[i]
		item.Reason = cleanWebField(item.Reason, &changed)
		item.Step = cleanWebField(item.Step, &changed)
		item.HowToUse = cleanWebField(item.HowToUse, &changed)
		item.Caution = cleanWebField(item.Caution, &changed)
		item.Benefits = cleanWebList(item.Benefits, &changed)
	}
	for i := range out.ProductGuidance {
		item := &out.ProductGuidance[i]
		item.Why = cleanWebField(item.Why, &changed)
		item.HowToUse = cleanWebField(item.HowToUse, &changed)
		item.Caution = cleanWebField(item.Caution, &changed)
		item.Benefits = cleanWebList(item.Benefits, &changed)
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
	protected := urlOrPathTokenRanges(s)
	changed := false
	droppedPrefix := false
	kept := false
	var b strings.Builder
	offset := 0
	for _, part := range parts {
		start := offset
		offset += len(part.text)
		if !part.word {
			if !kept && droppedPrefix && webLeadingPunct(part.text) {
				changed = true
				continue
			}
			b.WriteString(part.text)
			continue
		}
		if spanCovers(protected, start, offset) {
			b.WriteString(part.text)
			kept = true
			continue
		}
		if isWebProfanity(part.text) {
			changed = true
			if !kept {
				droppedPrefix = true
			}
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

func isWebProfanity(token string) bool {
	key := strings.ToLower(norm.NFC.String(token))
	if _, ok := webProfanityExact[key]; ok {
		return true
	}
	// Fold only đ. Other diacritics stay, so vài, lông, dư, and đeo are kept.
	plain := strings.ReplaceAll(key, "đ", "d")
	_, ok := webProfanityPlain[plain]
	return ok
}

func cleanWebZoneNotes(raw json.RawMessage, changed *bool) json.RawMessage {
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
		next := SanitizeWebCoachText(note)
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

func cleanWebScoreNotes(raw json.RawMessage, changed *bool) json.RawMessage {
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
		next := SanitizeWebCoachText(note)
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
