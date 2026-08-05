package ai

import "strings"

// NormalizeCareSuggestions cleans slot/step/why, caps length, and falls back from
// improvements when the model omitted care_suggestions (older prompts / partial JSON).
func NormalizeCareSuggestions(out *CoachStructuredOutput) []CoachCareSuggestion {
	if out == nil {
		return nil
	}
	raw := out.CareSuggestions
	if len(raw) == 0 {
		raw = careSuggestionsFromImprovements(out)
	}
	outList := make([]CoachCareSuggestion, 0, len(raw))
	for _, c := range raw {
		step := strings.TrimSpace(c.Step)
		why := strings.TrimSpace(c.Why)
		if step == "" {
			continue
		}
		slot := normalizeCareSlot(c.Slot)
		note := strings.TrimSpace(c.SafetyNote)
		outList = append(outList, CoachCareSuggestion{
			Slot:       slot,
			Step:       step,
			Why:        why,
			SafetyNote: note,
		})
		if len(outList) >= 5 {
			break
		}
	}
	out.CareSuggestions = outList
	return outList
}

func normalizeCareSlot(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "morning", "am", "sang", "sáng":
		return "morning"
	case "evening", "pm", "toi", "tối", "toi:", "tối:":
		return "evening"
	case "today", "uu_tien", "ưu tiên", "priority", "now":
		return "today"
	default:
		// Prefix match for values like "morning checklist" — avoid bare "am"/"pm"
		// (too short; would false-positive unrelated words).
		if strings.HasPrefix(s, "sáng") || strings.HasPrefix(s, "sang") || strings.HasPrefix(s, "morning") {
			return "morning"
		}
		if strings.HasPrefix(s, "tối") || strings.HasPrefix(s, "toi") || strings.HasPrefix(s, "evening") {
			return "evening"
		}
		return "today"
	}
}

func careSuggestionsFromImprovements(out *CoachStructuredOutput) []CoachCareSuggestion {
	if out == nil || len(out.Improvements) == 0 {
		return nil
	}
	list := make([]CoachCareSuggestion, 0, len(out.Improvements))
	for _, imp := range out.Improvements {
		tip := strings.TrimSpace(imp.Tip)
		if tip == "" {
			continue
		}
		slot := "today"
		step := tip
		lower := strings.ToLower(tip)
		if strings.HasPrefix(lower, "sáng:") || strings.HasPrefix(lower, "sang:") || strings.HasPrefix(lower, "am:") || strings.HasPrefix(lower, "morning:") {
			slot = "morning"
			if i := strings.Index(tip, ":"); i >= 0 && i+1 < len(tip) {
				step = strings.TrimSpace(tip[i+1:])
			}
		} else if strings.HasPrefix(lower, "tối:") || strings.HasPrefix(lower, "toi:") || strings.HasPrefix(lower, "pm:") || strings.HasPrefix(lower, "evening:") {
			slot = "evening"
			if i := strings.Index(tip, ":"); i >= 0 && i+1 < len(tip) {
				step = strings.TrimSpace(tip[i+1:])
			}
		}
		if step == "" {
			step = tip
		}
		list = append(list, CoachCareSuggestion{
			Slot: slot,
			Step: step,
			Why:  strings.TrimSpace(imp.Why),
		})
		if len(list) >= 5 {
			break
		}
	}
	return list
}
