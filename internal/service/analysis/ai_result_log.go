package analysis

import (
	"log/slog"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/google/uuid"
)

// aiResultLog is the measurement line for one photo skin-check analysis.
// Every string is clamped to a fixed vocabulary before it is logged.
// Email, photo keys, notes, tokens, and the raw User-Agent never belong here.
type aiResultLog struct {
	CheckID       uuid.UUID
	Confidence    string
	DeviceType    string
	PhotoPresent  bool
	AILatencyMs   int64
	Outcome       string
	Fallback      bool
	PhotoEvidence string
	LowReasonPath string
	PhotoNoteTag  string
	ClarifyCount  int
	ClarifyKind   string
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
// device_type is the client class: web-android, web-ios, web-desktop,
// android-app, or unknown. android-app is the native app, hyphenated like
// web-android and web-ios. The raw User-Agent is not a device_type.
// ai_latency_ms is vision time plus text-coach time, in milliseconds.
// outcome is completed or failed. fallback is true when vision could not run
// or the text coach answered from the secondary provider.
// photo_evidence is ok, limited, or skip (ClassifyCheckInPhotoEvidence).
// unknown means the vision pass had not finished.
// low_reason_path is photo (limited or skip), clarify_pending (photo ok and
// touch/duration questions still unanswered), or none.
// photo_note_tag is blur, dark, crop, shadow, unreadable, other, or none.
// clarify_count is how many clarify_questions were stored. clarify_kind is
// retake_tips, touch_questions, or none. Question text and photo notes are not
// logged.
func skinCheckAIResultArgs(in aiResultLog) []any {
	return []any{
		"check_id", in.CheckID.String(),
		"confidence", clampConfidence(in.Confidence),
		"device_type", clampDeviceType(in.DeviceType),
		"photo_present", in.PhotoPresent,
		"ai_latency_ms", clampLatency(in.AILatencyMs),
		"outcome", clampOutcome(in.Outcome),
		"fallback", in.Fallback,
		"photo_evidence", clampPhotoEvidence(in.PhotoEvidence),
		"low_reason_path", clampLowReasonPath(in.LowReasonPath),
		"photo_note_tag", clampPhotoNoteTag(in.PhotoNoteTag),
		"clarify_count", clampClarifyCount(in.ClarifyCount),
		"clarify_kind", clampClarifyKind(in.ClarifyKind),
	}
}

// applyLowConfidenceLogFields fills the reason fields from the vision pass and
// the clarify_questions stored on the score map. It runs only after vision has
// finished. Note text on the score map is ignored.
func applyLowConfidenceLogFields(in *aiResultLog, visionStatus, visionRaw string, labels map[string]any) {
	if in == nil {
		return
	}
	ev := ai.ClassifyCheckInPhotoEvidence(visionStatus, visionRaw)
	count := clarifyQuestionCount(labels)
	kind := clarifyKindForLog(ev.Kind, count)
	in.PhotoEvidence = ev.Kind
	in.PhotoNoteTag = ai.PhotoNoteTagFromVision(visionRaw)
	in.ClarifyCount = count
	in.ClarifyKind = kind
	in.LowReasonPath = lowReasonPath(ev.Kind, kind)
}

// lowReasonPath reports why a check-in can score low or medium.
// photo: CheckInConfidence took a photo-evidence branch (limited or skip).
// clarify_pending: the photo is ok and touch/duration questions are still unanswered.
// none: the photo is ok and nothing is waiting, or the vision pass did not run.
func lowReasonPath(photoEvidence, clarifyKind string) string {
	switch photoEvidence {
	case ai.PhotoEvidenceLimited, ai.PhotoEvidenceSkip:
		return "photo"
	case ai.PhotoEvidenceOK:
		if clarifyKind == "touch_questions" {
			return "clarify_pending"
		}
		return "none"
	default:
		return "none"
	}
}

// clarifyKindForLog is retake_tips on the bad-photo path when questions were
// stored, touch_questions when the photo is ok and questions remain, else none.
func clarifyKindForLog(photoEvidence string, count int) string {
	if count <= 0 {
		return "none"
	}
	switch photoEvidence {
	case ai.PhotoEvidenceLimited:
		return "retake_tips"
	case ai.PhotoEvidenceOK:
		return "touch_questions"
	default:
		return "none"
	}
}

func clarifyQuestionCount(labels map[string]any) int {
	if labels == nil {
		return 0
	}
	qs, ok := labels["clarify_questions"].([]string)
	if !ok {
		return 0
	}
	n := 0
	for _, q := range qs {
		if strings.TrimSpace(q) != "" {
			n++
		}
	}
	return n
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

func clampPhotoEvidence(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case ai.PhotoEvidenceOK, ai.PhotoEvidenceLimited, ai.PhotoEvidenceSkip:
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "unknown"
	}
}

func clampLowReasonPath(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "photo", "clarify_pending", "none":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "none"
	}
}

func clampPhotoNoteTag(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "blur", "dark", "crop", "shadow", "unreadable", "other", "none":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "none"
	}
}

func clampClarifyKind(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "retake_tips", "touch_questions", "none":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "none"
	}
}

func clampClarifyCount(n int) int {
	if n < 0 {
		return 0
	}
	return n
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
