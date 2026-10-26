package analysis

import (
	"log/slog"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// aiResultLog is the measurement line for one photo skin-check analysis.
// Every string is clamped to a fixed vocabulary before it is logged.
// Email, photo keys, notes, tokens, and the raw User-Agent never belong here.
type aiResultLog struct {
	CheckID      uuid.UUID
	Confidence   string
	DeviceType   string
	PhotoPresent bool
	AILatencyMs  int64
	Outcome      string
	Fallback     bool
}

// logSkinCheckAIResult emits one structured line for a photo check-in.
// Skip-mode (no photo) is not logged. The message is the event name.
func logSkinCheckAIResult(in aiResultLog) {
	if !in.PhotoPresent {
		return
	}
	slog.Info("skin_check_ai_result", skinCheckAIResultArgs(in)...)
}

// skinCheckAIResultArgs is the only set of fields on skin_check_ai_result.
// check_id is the skin-check UUID. confidence is low, medium, high, or unknown.
// device_type is web-android, web-ios, web-desktop, android-app, or unknown.
// ai_latency_ms is vision time plus text-coach time, in milliseconds.
// outcome is completed or failed. fallback is true when vision could not run
// or the text coach answered from the secondary provider.
func skinCheckAIResultArgs(in aiResultLog) []any {
	return []any{
		"check_id", in.CheckID.String(),
		"confidence", clampConfidence(in.Confidence),
		"device_type", clampDeviceType(in.DeviceType),
		"photo_present", in.PhotoPresent,
		"ai_latency_ms", clampLatency(in.AILatencyMs),
		"outcome", clampOutcome(in.Outcome),
		"fallback", in.Fallback,
	}
}

func deviceTypeForLog(passed, clientKind string) string {
	switch strings.TrimSpace(passed) {
	case "web-android", "web-ios", "web-desktop", "android-app", "unknown":
		return strings.TrimSpace(passed)
	}
	if domain.NormalizeRefreshClient(clientKind) == domain.RefreshClientAndroid {
		return "android-app"
	}
	return "unknown"
}

func clampDeviceType(v string) string {
	switch strings.TrimSpace(v) {
	case "web-android", "web-ios", "web-desktop", "android-app", "unknown":
		return strings.TrimSpace(v)
	default:
		return "unknown"
	}
}

func clampConfidence(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "unknown"
	}
}

func clampOutcome(v string) string {
	switch strings.TrimSpace(v) {
	case "completed", "failed":
		return strings.TrimSpace(v)
	default:
		return "unknown"
	}
}

func clampLatency(ms int64) int64 {
	if ms < 0 {
		return 0
	}
	return ms
}

// analysisUsedFallback reports a completed or failed photo analysis that did
// not stay on the primary vision + primary text-coach path.
func analysisUsedFallback(modelVersion, visionStatus string) bool {
	if strings.Contains(modelVersion, ",fallback") {
		return true
	}
	switch strings.TrimSpace(visionStatus) {
	case "unavailable", "no_openai_key":
		return true
	default:
		return false
	}
}

func confidenceFromLabels(labels map[string]any) string {
	if labels == nil {
		return ""
	}
	s, _ := labels["confidence"].(string)
	return s
}
