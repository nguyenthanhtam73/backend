package ai

import "strings"

// ExtractJSONObject trims optional ```json fences from model text.
func ExtractJSONObject(text string) ([]byte, error) {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "```") {
		t = strings.TrimPrefix(t, "```")
		t = strings.TrimPrefix(t, "json")
		t = strings.TrimSpace(t)
		if idx := strings.LastIndex(t, "```"); idx >= 0 {
			t = strings.TrimSpace(t[:idx])
		}
	}
	return []byte(t), nil
}
