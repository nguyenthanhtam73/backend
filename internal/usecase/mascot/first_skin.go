package mascot

import (
	"encoding/json"
	"strings"
)

// Drawable concerns. The web face only has gentle marks for these five.
var drawableConcerns = map[string]struct{}{
	"acne": {}, "oily": {}, "dry": {}, "red": {}, "dark_spot": {},
}

// MarksFromOnboarding reads the first skin check: the onboarding snapshot
// (photo analysis plus the concerns the person picked). Later check-ins are
// a different path. Levels stay light or medium so the face stays gentle.
func MarksFromOnboarding(snapshot, concerns json.RawMessage) []Mark {
	level := gentleLevel("")
	var tags []string
	tags = append(tags, stringList(concerns)...)

	var snap map[string]any
	if len(snapshot) > 0 && json.Unmarshal(snapshot, &snap) == nil && snap != nil {
		tags = append(tags, stringListRaw(snap["concerns"])...)
		tags = append(tags, stringListRaw(snap["body_concerns"])...)
		if analysis, ok := snap["skin_analysis"].(map[string]any); ok {
			level = gentleLevel(asString(analysis["severity_level"]))
			tags = append(tags, stringListRaw(analysis["concerns"])...)
			tags = append(tags, stringListRaw(analysis["main_concerns"])...)
			tags = append(tags, stringListRaw(analysis["concern_types"])...)
			if group := asString(analysis["morphology_group"]); group != "" {
				tags = append(tags, group)
			}
			if obs, ok := analysis["skin_observations"].(map[string]any); ok {
				tags = append(tags, observationTags(obs)...)
			}
		}
	}

	seen := map[string]struct{}{}
	var out []Mark
	for _, tag := range tags {
		id, ok := canonicalConcern(tag)
		if !ok {
			continue
		}
		if _, draw := drawableConcerns[id]; !draw {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, Mark{ID: id, Level: level})
	}
	sortMarks(out)
	return out
}

func gentleLevel(raw string) string {
	switch mapSeverity(raw) {
	case levelMedium, levelHeavy:
		return levelMedium
	default:
		return levelLight
	}
}

func sortMarks(marks []Mark) {
	for i := 1; i < len(marks); i++ {
		j := i
		for j > 0 && concernPos(marks[j].ID) < concernPos(marks[j-1].ID) {
			marks[j], marks[j-1] = marks[j-1], marks[j]
			j--
		}
	}
}

func stringList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var items []string
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	return items
}

func stringListRaw(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := asString(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return typed
	default:
		return nil
	}
}

func asString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func observationTags(obs map[string]any) []string {
	var tags []string
	if concern := observationConcern(asString(obs["acne_status"]), "acne", "mun", "mụn", "comedone", "bump", "pimple", "inflammatory", "active"); concern != "" {
		tags = append(tags, concern)
	}
	oil := strings.ToLower(asString(obs["oiliness_level"]))
	switch {
	case skippedObservation(oil):
	case strings.Contains(oil, "dry") || strings.Contains(oil, "khô") || strings.Contains(oil, "kho"):
		tags = append(tags, "dry")
	case strings.Contains(oil, "oil") || strings.Contains(oil, "dầu") || strings.Contains(oil, "dau") || strings.Contains(oil, "bóng") || strings.Contains(oil, "moderate") || strings.Contains(oil, "high"):
		tags = append(tags, "oily")
	}
	if concern := observationConcern(asString(obs["redness"]), "red", "red", "đỏ", "high", "mild", "moderate"); concern != "" {
		tags = append(tags, concern)
	}
	if concern := observationConcern(asString(obs["pigmentation"]), "dark_spot", "pigment", "thâm", "tham", "spot", "mild", "moderate", "high"); concern != "" {
		tags = append(tags, concern)
	}
	return tags
}

func observationConcern(raw, concern string, hints ...string) string {
	low := strings.ToLower(strings.TrimSpace(raw))
	if skippedObservation(low) {
		return ""
	}
	for _, hint := range hints {
		if strings.Contains(low, hint) {
			return concern
		}
	}
	return ""
}

func skippedObservation(low string) bool {
	if low == "" {
		return true
	}
	switch low {
	case "none", "no", "clear", "normal", "không", "khong", "không có", "khong co":
		return true
	default:
		return strings.HasPrefix(low, "none") || strings.HasPrefix(low, "no ") || strings.Contains(low, "không có")
	}
}
