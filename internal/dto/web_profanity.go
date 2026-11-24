package dto

import (
	"encoding/json"
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// CleanWebProfanity rewrites one sentence before it is sent to a client.
// internal/service/ai installs the swear filter at init. Numbers and
// pronouns are left alone. Stored rows are not written back.
var CleanWebProfanity = func(s string) string { return s }

// WebPolishFromCheck is what the web screen may use from a stored check.
// A missing check is Vietnamese with no user note.
func WebPolishFromCheck(c *domain.SkinCheck) WebCoachPolish {
	if c == nil {
		return WebCoachPolish{Locale: "vi", NoteKnown: true}
	}
	return WebCoachPolish{
		Locale:      UILocale(c.ClimateContext),
		NoteKnown:   true,
		HasUserNote: strings.TrimSpace(c.UserNote) != "",
	}
}

// UILocale is vi unless climate_context says ui_locale=en.
func UILocale(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "vi"
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return "vi"
	}
	v, _ := m["ui_locale"].(string)
	if strings.EqualFold(strings.TrimSpace(v), "en") {
		return "en"
	}
	return "vi"
}

// WebCoachPolish is the bit of the check the web read path may use.
// Locale is vi or en. NoteKnown is true when the stored user note was loaded.
type WebCoachPolish struct {
	Locale      string
	NoteKnown   bool
	HasUserNote bool
}

// PolishWebCoachDetail rewrites a whole web screen (mày budget, one question,
// Vietnamese notes). Android checks do not call it. ai installs it at init.
var PolishWebCoachDetail = func(*SkinCoachDetail, WebCoachPolish) {}

func cleanWebProfanityDetail(d *SkinCoachDetail) {
	if d == nil || CleanWebProfanity == nil {
		return
	}
	d.SummaryNotes = cleanWebOne(d.SummaryNotes)
	d.SituationSummary = cleanWebOne(d.SituationSummary)
	d.ConcernAlignment = cleanWebOne(d.ConcernAlignment)
	d.MedicalDisclaimer = cleanWebOne(d.MedicalDisclaimer)
	d.PhotoLimitedNote = cleanWebOne(d.PhotoLimitedNote)
	d.Strengths = cleanWebList(d.Strengths)
	d.RoutineHints = cleanWebList(d.RoutineHints)
	d.AvoidOrPatch = cleanWebList(d.AvoidOrPatch)
	d.SafetyReminders = cleanWebList(d.SafetyReminders)
	d.ClarifyQuestions = cleanWebList(d.ClarifyQuestions)
	for i := range d.Improvements {
		d.Improvements[i].Tip = cleanWebOne(d.Improvements[i].Tip)
		d.Improvements[i].Why = cleanWebOne(d.Improvements[i].Why)
	}
	for i := range d.CareSuggestions {
		d.CareSuggestions[i].Step = cleanWebOne(d.CareSuggestions[i].Step)
		d.CareSuggestions[i].Why = cleanWebOne(d.CareSuggestions[i].Why)
		d.CareSuggestions[i].SafetyNote = cleanWebOne(d.CareSuggestions[i].SafetyNote)
	}
	for i := range d.ProductSuggestions {
		item := &d.ProductSuggestions[i]
		item.Reason = cleanWebOne(item.Reason)
		item.Step = cleanWebOne(item.Step)
		item.HowToUse = cleanWebOne(item.HowToUse)
		item.Caution = cleanWebOne(item.Caution)
		item.Benefits = cleanWebList(item.Benefits)
	}
	for i := range d.ProductGuidance {
		item := &d.ProductGuidance[i]
		item.Why = cleanWebOne(item.Why)
		item.Step = cleanWebOne(item.Step)
		item.HowToUse = cleanWebOne(item.HowToUse)
		item.Caution = cleanWebOne(item.Caution)
		item.Benefits = cleanWebList(item.Benefits)
	}
	if len(d.ZoneNotes) > 0 {
		notes := make([]CoachZoneNote, 0, len(d.ZoneNotes))
		for _, n := range d.ZoneNotes {
			n.Note = cleanWebOne(n.Note)
			if strings.TrimSpace(n.Note) == "" {
				continue
			}
			notes = append(notes, n)
		}
		d.ZoneNotes = notes
	}
	if d.SkinScoreNotes != nil {
		n := d.SkinScoreNotes
		n.Overall = cleanWebOne(n.Overall)
		n.Hydration = cleanWebOne(n.Hydration)
		n.Clarity = cleanWebOne(n.Clarity)
		n.Barrier = cleanWebOne(n.Barrier)
		if n.Overall == "" && n.Hydration == "" && n.Clarity == "" && n.Barrier == "" {
			d.SkinScoreNotes = nil
		}
	}
}

func cleanWebOne(s string) string {
	if CleanWebProfanity == nil {
		return s
	}
	return strings.TrimSpace(CleanWebProfanity(s))
}

func cleanWebList(items []string) []string {
	if len(items) == 0 {
		return items
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = cleanWebOne(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
