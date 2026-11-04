package repository

import (
	"encoding/json"
	"strings"
)

// routineAllStepsDone reports whether every stored morning and evening step
// is ticked. An empty checklist is not done. Legacy string lists are not
// done. Blank steps (no id and no title) are ignored. This matches the
// mascot checklist decoder.
func routineAllStepsDone(morning, evening json.RawMessage) bool {
	steps := append(storedStepDone(morning), storedStepDone(evening)...)
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

func storedStepDone(raw json.RawMessage) []bool {
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
