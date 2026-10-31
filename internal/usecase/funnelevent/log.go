package funnelevent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"math"
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// checkinPhotoLogEvents is the only set of funnel events written to the
// server log. checkin_photo_staged is the denominator for the blur warning.
// Every other accepted event is stored and stays out of this line.
var checkinPhotoLogEvents = map[string]struct{}{
	domain.FunnelCheckinPhotoBlurShown:  {},
	domain.FunnelCheckinPhotoBlurChoice: {},
	domain.FunnelCheckinPhotoStaged:     {},
}

// Coarse client classes. Same vocabulary as skincheck.ClassifyDevice.
// unknown is omitted: a missing device is not a measurement.
const (
	logDeviceWebAndroid = "web-android"
	logDeviceWebIOS     = "web-ios"
	logDeviceWebDesktop = "web-desktop"
	logDeviceAndroidApp = "android-app"
)

// maxBlurScore drops values that cannot be a contrast-normalized Laplacian
// variance. Those scores sit well below 1; a large number is not logged.
const maxBlurScore = 1000

// logCheckinPhotoFunnel emits one structured line for an accepted photo
// funnel event. The message is funnel_event. Attributes are the event name
// plus slot, score, choice, via, platform, and device when the value is
// present and on the safe vocabulary.
//
// Emails, user ids, session ids, paths, photo URLs, filenames, and the raw
// User-Agent are never arguments. device must already be a ClassifyDevice
// result; any other string is dropped.
func logCheckinPhotoFunnel(event string, props json.RawMessage, device string) {
	if _, ok := checkinPhotoLogEvents[event]; !ok {
		return
	}
	slog.Info("funnel_event", checkinPhotoFunnelArgs(event, props, device)...)
}

func checkinPhotoFunnelArgs(event string, props json.RawMessage, device string) []any {
	args := []any{"event", event}
	obj := flatFunnelProps(props)
	if slot, ok := propSlot(obj["slot"]); ok {
		args = append(args, "slot", slot)
	}
	if score, ok := propScore(obj["score"]); ok {
		args = append(args, "score", score)
	}
	if choice, ok := propChoice(obj["choice"]); ok {
		args = append(args, "choice", choice)
	}
	if via, ok := propVia(obj["via"]); ok {
		args = append(args, "via", via)
	}
	if platform, ok := propDeviceClass(obj["platform"]); ok {
		args = append(args, "platform", platform)
	}
	if logged, ok := loggedDevice(obj, device); ok {
		args = append(args, "device", logged)
	}
	return args
}

// loggedDevice prefers an explicit device or device_type prop. A value that
// is not a coarse class is ignored, and the classified request device is
// used instead. The raw User-Agent never matches the class list.
func loggedDevice(obj map[string]json.RawMessage, classified string) (string, bool) {
	if d, ok := propDeviceClass(obj["device"]); ok {
		return d, true
	}
	if d, ok := propDeviceClass(obj["device_type"]); ok {
		return d, true
	}
	return safeDeviceClass(classified)
}

func flatFunnelProps(raw json.RawMessage) map[string]json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(raw), &obj); err != nil {
		return nil
	}
	return obj
}

func propSlot(raw json.RawMessage) (int, bool) {
	n, ok := propNumber(raw)
	if !ok || (n != 0 && n != 1) {
		return 0, false
	}
	return int(n), true
}

func propScore(raw json.RawMessage) (float64, bool) {
	n, ok := propNumber(raw)
	if !ok || n < 0 || n > maxBlurScore {
		return 0, false
	}
	return math.Round(n*100) / 100, true
}

func propChoice(raw json.RawMessage) (string, bool) {
	s, ok := propString(raw)
	if !ok {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "retake", "send_anyway":
		return strings.ToLower(strings.TrimSpace(s)), true
	default:
		return "", false
	}
}

func propVia(raw json.RawMessage) (string, bool) {
	s, ok := propString(raw)
	if !ok {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "button", "submit":
		return strings.ToLower(strings.TrimSpace(s)), true
	default:
		return "", false
	}
}

func propDeviceClass(raw json.RawMessage) (string, bool) {
	s, ok := propString(raw)
	if !ok {
		return "", false
	}
	return safeDeviceClass(s)
}

func safeDeviceClass(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case logDeviceWebAndroid, logDeviceWebIOS, logDeviceWebDesktop, logDeviceAndroidApp:
		return strings.ToLower(strings.TrimSpace(v)), true
	default:
		return "", false
	}
}

func propString(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func propNumber(raw json.RawMessage) (float64, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] == '"' || raw[0] == 't' || raw[0] == 'f' || raw[0] == 'n' || raw[0] == '{' || raw[0] == '[' {
		return 0, false
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}
