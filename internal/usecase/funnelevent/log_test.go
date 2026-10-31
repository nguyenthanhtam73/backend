package funnelevent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
)

func TestCheckinPhotoFunnelArgs_SafeFieldsOnly(t *testing.T) {
	const poisoned = "user@dadiary.vn https://cdn.example/uploads/checks/face.jpg blob:https://dadiary.vn/abc Mozilla/5.0 r2.cloudflarestorage.com/bucket/key"
	props := json.RawMessage(`{
		"slot": 1,
		"score": 0.086,
		"choice": " SEND_ANYWAY ",
		"via": " Submit ",
		"platform": " web-ios ",
		"never_checked_in": true,
		"skip_mode": false,
		"signed_in": true,
		"count": 2,
		"email": "person@example.com",
		"user_id": "6b1c2a30-7c3e-4f2a-9d1b-0a1b2c3d4e5f",
		"session_id": "sess-private",
		"path": "/check-in?email=person@example.com",
		"photo_url": "https://cdn.example/uploads/checks/face.jpg",
		"url": "blob:https://dadiary.vn/abc",
		"filename": "IMG_1234.jpg",
		"note": "` + poisoned + `"
	}`)

	args := checkinPhotoFunnelArgs(domain.FunnelCheckinPhotoBlurChoice, props, "web-android")
	keys := argKeys(t, args)
	if strings.Join(keys, ",") != "event,slot,score,choice,via,platform,device" {
		t.Fatalf("keys %v", keys)
	}
	if args[1] != domain.FunnelCheckinPhotoBlurChoice || args[3] != 1 || args[5] != 0.09 || args[7] != "send_anyway" || args[9] != "submit" || args[11] != "web-ios" || args[13] != "web-android" {
		t.Fatalf("args %#v", args)
	}

	for _, handler := range []func(*bytes.Buffer) slog.Handler{
		func(buf *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(buf, nil) },
		func(buf *bytes.Buffer) slog.Handler { return slog.NewTextHandler(buf, nil) },
	} {
		var buf bytes.Buffer
		slog.New(handler(&buf)).Info("funnel_event", args...)
		line := buf.String()
		assertNoPrivateFunnelLog(t, line)
		if strings.Count(line, "funnel_event") != 1 || !strings.Contains(line, domain.FunnelCheckinPhotoBlurChoice) {
			t.Fatalf("line %s", line)
		}
	}
}

func TestCheckinPhotoFunnelArgs_ChoiceVocabularyAndDeviceFallback(t *testing.T) {
	retake := checkinPhotoFunnelArgs(domain.FunnelCheckinPhotoBlurChoice, json.RawMessage(`{
		"slot": 0,
		"score": 0.04,
		"choice": "retake",
		"via": "button",
		"device": "https://cdn.example/photo.jpg",
		"user_id": "6b1c2a30-7c3e-4f2a-9d1b-0a1b2c3d4e5f"
	}`), "android-app")
	if got := argMap(t, retake); got["choice"] != "retake" || got["via"] != "button" || got["device"] != "android-app" || got["slot"] != 0 || got["score"] != 0.04 {
		t.Fatalf("retake %#v", got)
	}
	if _, ok := argMap(t, retake)["user_id"]; ok {
		t.Fatal("logged user_id")
	}

	shown := checkinPhotoFunnelArgs(domain.FunnelCheckinPhotoBlurShown, json.RawMessage(`{"slot":0,"score":0.12,"device_type":" WEB-DESKTOP "}`), "web-android")
	if got := argMap(t, shown); got["device"] != "web-desktop" || got["event"] != domain.FunnelCheckinPhotoBlurShown {
		t.Fatalf("shown %#v", got)
	}
	if _, ok := argMap(t, shown)["choice"]; ok {
		t.Fatal("shown logged a choice")
	}

	staged := checkinPhotoFunnelArgs(domain.FunnelCheckinPhotoStaged, json.RawMessage(`{"slot":1,"count":2}`), "")
	if got := argMap(t, staged); len(got) != 2 || got["event"] != domain.FunnelCheckinPhotoStaged || got["slot"] != 1 {
		t.Fatalf("staged %#v", got)
	}

	rawUA := "Mozilla/5.0 (Linux; Android 14) user@dadiary.vn https://cdn.example/uploads/face.jpg"
	dropped := checkinPhotoFunnelArgs(domain.FunnelCheckinPhotoStaged, json.RawMessage(`{"slot":"0","score":"0.1","choice":"keep","via":"https://cdn.example/a.jpg","platform":"ios"}`), rawUA)
	if got := argMap(t, dropped); len(got) != 1 || got["event"] != domain.FunnelCheckinPhotoStaged {
		t.Fatalf("unsafe props logged %#v", got)
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("funnel_event", dropped...)
	assertNoPrivateFunnelLog(t, buf.String())
	if strings.Contains(buf.String(), "Android") || strings.Contains(buf.String(), "Mozilla") {
		t.Fatalf("raw user agent logged: %s", buf.String())
	}
}

func TestCheckinPhotoFunnelArgs_DropsOutOfRangeNumbers(t *testing.T) {
	props := json.RawMessage(`{"slot":2,"score":1001,"choice":"retake user@dadiary.vn","via":"button"}`)
	got := argMap(t, checkinPhotoFunnelArgs(domain.FunnelCheckinPhotoBlurChoice, props, "unknown"))
	if _, ok := got["slot"]; ok {
		t.Fatal("logged slot")
	}
	if _, ok := got["score"]; ok {
		t.Fatal("logged score")
	}
	if _, ok := got["choice"]; ok {
		t.Fatal("logged choice")
	}
	if got["via"] != "button" || got["device"] != nil {
		t.Fatalf("args %#v", got)
	}
}

func TestLogCheckinPhotoFunnel_OneLinePerAllowlistedEvent(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	logCheckinPhotoFunnel(domain.FunnelCheckinPhotoBlurShown, json.RawMessage(`{"slot":0,"score":0.09}`), "web-android")
	logCheckinPhotoFunnel(domain.FunnelCheckinPhotoBlurChoice, json.RawMessage(`{"slot":0,"score":0.09,"choice":"retake","via":"button"}`), "web-android")
	logCheckinPhotoFunnel(domain.FunnelCheckinPhotoStaged, json.RawMessage(`{"slot":1,"count":1}`), "web-ios")
	logCheckinPhotoFunnel(domain.FunnelCheckinPageView, json.RawMessage(`{"slot":0,"score":0.09,"choice":"retake","via":"button","email":"person@example.com","photo_url":"https://cdn.example/a.jpg"}`), "web-android")
	logCheckinPhotoFunnel(domain.FunnelCheckinSubmitSuccess, json.RawMessage(`{"photo_count":1}`), "web-desktop")

	lines := funnelEventLines(t, buf.String())
	if len(lines) != 3 {
		t.Fatalf("lines=%d body=%s", len(lines), buf.String())
	}
	if lines[0]["event"] != domain.FunnelCheckinPhotoBlurShown || lines[1]["event"] != domain.FunnelCheckinPhotoBlurChoice || lines[2]["event"] != domain.FunnelCheckinPhotoStaged {
		t.Fatalf("events %#v", lines)
	}
	if lines[1]["choice"] != "retake" || lines[1]["via"] != "button" {
		t.Fatalf("choice line %#v", lines[1])
	}
	assertNoPrivateFunnelLog(t, buf.String())
	if strings.Contains(buf.String(), domain.FunnelCheckinPageView) || strings.Contains(buf.String(), domain.FunnelCheckinSubmitSuccess) {
		t.Fatalf("logged an event outside the allow-list: %s", buf.String())
	}
}

func argKeys(t *testing.T, args []any) []string {
	t.Helper()
	keys := make([]string, 0, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			t.Fatalf("arg %d key type %T", i, args[i])
		}
		keys = append(keys, key)
	}
	return keys
}

func argMap(t *testing.T, args []any) map[string]any {
	t.Helper()
	out := make(map[string]any, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			t.Fatalf("arg %d key type %T", i, args[i])
		}
		out[key] = args[i+1]
	}
	return out
}

func funnelEventLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("decode %s: %v", line, err)
		}
		if obj["msg"] != "funnel_event" {
			continue
		}
		lines = append(lines, obj)
	}
	return lines
}

func assertNoPrivateFunnelLog(t *testing.T, line string) {
	t.Helper()
	lower := strings.ToLower(line)
	for _, bad := range []string{
		"@",
		"http://",
		"https://",
		"blob:",
		"uploads/",
		"r2.cloudflarestorage",
		"mozilla/",
		"user-agent",
		".jpg",
		"checks/",
		"person@example.com",
		"user@dadiary.vn",
		"6b1c2a30-7c3e-4f2a-9d1b-0a1b2c3d4e5f",
		"sess-private",
		"img_1234",
		"photo_url",
		"user_id",
		"session_id",
		"email",
	} {
		if strings.Contains(lower, bad) {
			t.Fatalf("log contains %q: %s", bad, line)
		}
	}
}
