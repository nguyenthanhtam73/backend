package ai

import (
	"encoding/json"
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

const maxClarifyQuestions = 3

// CheckInConfidence scores a photo check-in and lists follow-up questions.
// It reuses ClassifyMorphology, MorphologyClarifyQuestions, and RetakePhotoTips.
// No model call. Questions the user already answered in skin_context are removed.
// A close-up drops the "fill the frame / chụp sát vùng" retake tip.
func CheckInConfidence(ev CheckInPhotoEvidence, visionRaw string, photoCtx json.RawMessage, locale string) (level string, needsMore bool, questions []string) {
	switch ev.Kind {
	case PhotoEvidenceSkip:
		level = ConfidenceLow
	case PhotoEvidenceLimited:
		level = ConfidenceLow
		questions = RetakePhotoTips(locale)
		if photoIsCloseUp(photoCtx) {
			questions = withoutCloseUpFramingTip(questions)
		}
	default:
		level = ConfidenceHigh
	}

	if ev.Kind == PhotoEvidenceSkip {
		return level, false, nil
	}

	answered := answeredSkinCues(photoCtx)
	levels := []string{level}
	for _, z := range zoneObservationValues(visionRaw) {
		region := morphologyRegion(z.Zone)
		verdict := ClassifyMorphology(MorphologyFeaturesFromProse(z.Cue, region))
		levels = append(levels, verdict.Confidence)
		if !ShouldAskUser(verdict) {
			continue
		}
		filtered := verdict
		filtered.MissingCues = withoutAnsweredCues(verdict.MissingCues, answered)
		questions = appendQuestions(questions, MorphologyClarifyQuestions(filtered, locale))
	}
	if len(questions) > maxClarifyQuestions {
		questions = questions[:maxClarifyQuestions]
	}
	if len(questions) == 0 {
		questions = nil
	}
	return WorstConfidence(levels...), len(questions) > 0, questions
}

func morphologyRegion(zone string) string {
	switch strings.ToLower(strings.TrimSpace(zone)) {
	case "left_cheek", "right_cheek":
		return "cheek"
	case "around_mouth":
		return "perioral"
	case "jawline":
		return "jaw"
	default:
		return strings.ToLower(strings.TrimSpace(zone))
	}
}

func answeredSkinCues(photoCtx json.RawMessage) map[string]struct{} {
	doc := dto.DecodePhotoContext(photoCtx)
	if doc.SkinContext == nil {
		return nil
	}
	sc := doc.SkinContext
	out := map[string]struct{}{}
	if sc.Firmness != "" && sc.Firmness != "unknown" {
		out[CueTouchFirmness] = struct{}{}
	}
	if sc.Duration != "" && sc.Duration != "unknown" {
		out[CueDuration] = struct{}{}
	}
	if sc.Pain != "" && sc.Pain != "unknown" {
		out[CuePain] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func withoutAnsweredCues(cues []string, answered map[string]struct{}) []string {
	if len(answered) == 0 {
		return cues
	}
	out := make([]string, 0, len(cues))
	for _, c := range cues {
		if _, skip := answered[c]; skip {
			continue
		}
		out = append(out, c)
	}
	return out
}

func photoIsCloseUp(photoCtx json.RawMessage) bool {
	doc := dto.DecodePhotoContext(photoCtx)
	for _, img := range doc.Images {
		if img.Kind == dto.PhotoKindCloseup {
			return true
		}
	}
	return false
}

func withoutCloseUpFramingTip(tips []string) []string {
	if len(tips) == 0 {
		return nil
	}
	out := make([]string, 0, len(tips))
	for _, tip := range tips {
		low := strings.ToLower(tip)
		if strings.Contains(low, "sát vùng") || strings.Contains(low, "fill the frame") {
			continue
		}
		out = append(out, tip)
	}
	return out
}

func appendQuestions(dst, more []string) []string {
	if len(dst) >= maxClarifyQuestions {
		return dst
	}
	seen := make(map[string]struct{}, len(dst))
	for _, q := range dst {
		seen[strings.ToLower(q)] = struct{}{}
	}
	for _, q := range more {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		key := strings.ToLower(q)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		dst = append(dst, q)
		if len(dst) >= maxClarifyQuestions {
			break
		}
	}
	return dst
}
