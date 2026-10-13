package ai

// coach_prompt.go — selector for the daily skincare coach system prompt.
//
// v30: every client addresses the user as bạn. There is no account setting for
// a mày/tao style. Beginner ("Người mới") only softens how severe the wording
// is. The prompt text lives in coach_prompt_polite.go.

import (
	"encoding/json"
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// MinVisionDetailCitations is the minimum photo-specific details required when vision is available.
const MinVisionDetailCitations = 4

// MaxCoachValidationRetries is how many times to re-prompt the coach when output fails validation.
// Each retry is a full coach regeneration (~15–30s) and is the single biggest lever on total
// wall time. Set to 0: we take the coach's first output as final so the job stays well under
// the 120s frontend polling timeout. Quality is defended up-front by a strict system prompt +
// per-turn checklist instead of by a costly second generation.
const MaxCoachValidationRetries = 0

// GetCoachPrompt returns the daily coach system prompt. Skill level changes
// depth only. Address is always mình/bạn.
func GetCoachPrompt(skillLevel string) string {
	return androidCoachPrompt(skillLevel)
}

// ResolveCoachSkillLevel chọn skill tag: climate_context → profile → "intermediate".
func ResolveCoachSkillLevel(check *domain.SkinCheck, profile *domain.SkinProfile) string {
	if check != nil && len(check.ClimateContext) > 0 {
		var m map[string]any
		if err := json.Unmarshal(check.ClimateContext, &m); err == nil && m != nil {
			if v, ok := m["coach_skill_level"].(string); ok {
				if tag := normalizeCoachSkillTag(v); tag != "" {
					return tag
				}
			}
		}
	}
	if profile != nil && profile.SkillLevel != "" && profile.SkillLevel != domain.SkillLevelUnspecified {
		if tag := normalizeCoachSkillTag(string(profile.SkillLevel)); tag != "" {
			return tag
		}
	}
	return "intermediate"
}

// GetCoachPromptForClient returns the daily coach prompt. clientKind no longer
// selects a voice: web, android, and empty all use mình/bạn. The argument stays
// so callers that already pass the stored skin-check kind do not change.
func GetCoachPromptForClient(skillLevel, _ string) string {
	return GetCoachPrompt(skillLevel)
}

// IsAndroidCoachVoice reports whether clientKind is the Play app. Voice text is
// the same for every client; this still tags model_version with "+android".
func IsAndroidCoachVoice(clientKind string) bool {
	return domain.NormalizeRefreshClient(clientKind) == domain.RefreshClientAndroid
}

func normalizeCoachSkillTag(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "beginner", "intermediate", "advanced":
		return s
	default:
		return ""
	}
}
