package dto

import "strings"

// CleanPublicCoachText rewrites one sentence a person reads, before it is
// sent to a client. internal/service/ai installs the real filter at init.
// Until then the text is unchanged. Stored rows are not written back.
var CleanPublicCoachText = func(s string) string { return s }

func cleanPublicCoachDetail(d *SkinCoachDetail) {
	if d == nil || CleanPublicCoachText == nil {
		return
	}
	d.SummaryNotes = cleanOne(d.SummaryNotes)
	d.SituationSummary = cleanOne(d.SituationSummary)
	d.ConcernAlignment = cleanOne(d.ConcernAlignment)
	d.MedicalDisclaimer = cleanOne(d.MedicalDisclaimer)
	d.PhotoLimitedNote = cleanOne(d.PhotoLimitedNote)
	d.Strengths = cleanList(d.Strengths)
	d.RoutineHints = cleanList(d.RoutineHints)
	d.AvoidOrPatch = cleanList(d.AvoidOrPatch)
	d.SafetyReminders = cleanList(d.SafetyReminders)
	d.ClarifyQuestions = cleanList(d.ClarifyQuestions)
	for i := range d.Improvements {
		d.Improvements[i].Tip = cleanOne(d.Improvements[i].Tip)
		d.Improvements[i].Why = cleanOne(d.Improvements[i].Why)
	}
	for i := range d.CareSuggestions {
		d.CareSuggestions[i].Step = cleanOne(d.CareSuggestions[i].Step)
		d.CareSuggestions[i].Why = cleanOne(d.CareSuggestions[i].Why)
		d.CareSuggestions[i].SafetyNote = cleanOne(d.CareSuggestions[i].SafetyNote)
	}
	for i := range d.ProductSuggestions {
		item := &d.ProductSuggestions[i]
		item.Reason = cleanOne(item.Reason)
		item.Step = cleanOne(item.Step)
		item.HowToUse = cleanOne(item.HowToUse)
		item.Caution = cleanOne(item.Caution)
		item.Benefits = cleanList(item.Benefits)
	}
	for i := range d.ProductGuidance {
		item := &d.ProductGuidance[i]
		item.Why = cleanOne(item.Why)
		item.HowToUse = cleanOne(item.HowToUse)
		item.Caution = cleanOne(item.Caution)
		item.Benefits = cleanList(item.Benefits)
	}
	if len(d.ZoneNotes) > 0 {
		notes := make([]CoachZoneNote, 0, len(d.ZoneNotes))
		for _, n := range d.ZoneNotes {
			n.Note = cleanOne(n.Note)
			if strings.TrimSpace(n.Note) == "" {
				continue
			}
			notes = append(notes, n)
		}
		d.ZoneNotes = notes
	}
	if d.SkinScoreNotes != nil {
		n := d.SkinScoreNotes
		n.Overall = cleanOne(n.Overall)
		n.Hydration = cleanOne(n.Hydration)
		n.Clarity = cleanOne(n.Clarity)
		n.Barrier = cleanOne(n.Barrier)
		if n.Overall == "" && n.Hydration == "" && n.Clarity == "" && n.Barrier == "" {
			d.SkinScoreNotes = nil
		}
	}
}

func cleanOne(s string) string {
	if CleanPublicCoachText == nil {
		return s
	}
	return strings.TrimSpace(CleanPublicCoachText(s))
}

func cleanList(items []string) []string {
	if len(items) == 0 {
		return items
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = cleanOne(item)
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
