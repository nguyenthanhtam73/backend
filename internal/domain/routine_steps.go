package domain

import (
	"encoding/json"
	"strings"
)

// RoutineAllStepsDone reports whether every stored morning and evening step
// is ticked. An empty checklist is not done. A legacy list of strings is not
// done, because those rows have no completed flag. Blank steps (no id and no
// title) are ignored. This is the glow routine bonus and the Bé Da full-routine
// care day: both read this function so a day cannot count for one and not the
// other.
func RoutineAllStepsDone(morning, evening json.RawMessage) bool {
	steps := append(routineStoredSteps(morning), routineStoredSteps(evening)...)
	if len(steps) == 0 {
		return false
	}
	for _, done := range steps {
		if !done {
			return false
		}
	}
	return true
}

// routineStoredSteps returns one bool per step that counts. True means that
// step is ticked. The shape matches the mascot checklist decoder: objects
// need an id or a title, and legacy string arrays are unticked.
func routineStoredSteps(raw json.RawMessage) []bool {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
		return nil
	}
	var steps []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Completed bool   `json:"completed"`
	}
	if err := json.Unmarshal(raw, &steps); err != nil {
		var lines []string
		if err := json.Unmarshal(raw, &lines); err != nil {
			return nil
		}
		out := make([]bool, 0, len(lines))
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			out = append(out, false)
		}
		return out
	}
	out := make([]bool, 0, len(steps))
	for _, step := range steps {
		if strings.TrimSpace(step.Title) == "" && strings.TrimSpace(step.ID) == "" {
			continue
		}
		out = append(out, step.Completed)
	}
	return out
}
