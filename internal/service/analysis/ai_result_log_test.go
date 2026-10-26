package analysis

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestSkinCheckAIResultArgs_AllowlistAndNoPrivateText(t *testing.T) {
	id := uuid.MustParse("6b1c2a30-7c3e-4f2a-9d1b-0a1b2c3d4e5f")
	poisonedNote := "high user@dadiary.vn https://cdn.example/uploads/checks/face.jpg r2.cloudflarestorage.com/bucket/key"
	poisonedAgent := "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 user@secret.example https://r2.cloudflarestorage.com/photo Bearer abc.def"
	in := aiResultLog{
		CheckID:      id,
		Confidence:   poisonedNote,
		DeviceType:   poisonedAgent,
		PhotoPresent: true,
		AILatencyMs:  842,
		Outcome:      "completed see https://secret.example/token",
		Fallback:     true,
	}

	args := skinCheckAIResultArgs(in)
	keys := make([]string, 0, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			t.Fatalf("arg %d key type %T", i, args[i])
		}
		keys = append(keys, key)
	}
	wantKeys := []string{"check_id", "confidence", "device_type", "photo_present", "ai_latency_ms", "outcome", "fallback"}
	if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("keys %v", keys)
	}
	if args[1] != id.String() || args[3] != "unknown" || args[5] != "unknown" || args[7] != true || args[9] != int64(842) || args[11] != "unknown" || args[13] != true {
		t.Fatalf("clamped args %#v", args)
	}

	for _, handler := range []func(*bytes.Buffer) slog.Handler{
		func(buf *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(buf, nil) },
		func(buf *bytes.Buffer) slog.Handler { return slog.NewTextHandler(buf, nil) },
	} {
		var buf bytes.Buffer
		slog.New(handler(&buf)).Info("skin_check_ai_result", args...)
		line := buf.String()
		assertNoPrivateLogText(t, line)
		if !strings.Contains(line, "skin_check_ai_result") || !strings.Contains(line, id.String()) {
			t.Fatalf("missing event or check id: %s", line)
		}
		if strings.Count(line, "skin_check_ai_result") != 1 {
			t.Fatalf("expected one event: %s", line)
		}
	}
}

func TestSkinCheckAIResultArgs_KeepsValidMeasurementFields(t *testing.T) {
	id := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	args := skinCheckAIResultArgs(aiResultLog{
		CheckID:      id,
		Confidence:   " HIGH ",
		DeviceType:   " web-ios ",
		PhotoPresent: true,
		AILatencyMs:  1500,
		Outcome:      "completed",
		Fallback:     false,
	})
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("skin_check_ai_result", args...)
	line := buf.String()
	assertNoPrivateLogText(t, line)
	for _, want := range []string{
		`"confidence":"high"`,
		`"device_type":"web-ios"`,
		`"photo_present":true`,
		`"ai_latency_ms":1500`,
		`"outcome":"completed"`,
		`"fallback":false`,
		id.String(),
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %s in %s", want, line)
		}
	}
}

func TestLogSkinCheckAIResult_SkipsWhenNoPhoto(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	logSkinCheckAIResult(aiResultLog{
		CheckID:      uuid.New(),
		Confidence:   "high user@dadiary.vn https://cdn.example/a.jpg",
		DeviceType:   "Mozilla/5.0 user@x.com",
		PhotoPresent: false,
		Outcome:      "completed",
	})
	if buf.Len() != 0 {
		t.Fatalf("logged a check-in with no photo: %s", buf.String())
	}
}

func TestDeviceTypeForLog(t *testing.T) {
	cases := []struct {
		passed, kind, want string
	}{
		{"web-android", "", "web-android"},
		{"web-ios", domain.RefreshClientAndroid, "web-ios"},
		{"web-desktop", "", "web-desktop"},
		{"android-app", domain.RefreshClientWeb, "android-app"},
		{"unknown", domain.RefreshClientAndroid, "unknown"},
		{"", domain.RefreshClientAndroid, "android-app"},
		{"  android  ", domain.RefreshClientAndroid, "android-app"},
		{"https://cdn.example/uploads/a.jpg user@x.com", domain.RefreshClientWeb, "unknown"},
		{"Mozilla/5.0 (Linux; Android 14)", "", "unknown"},
		{"", "", "unknown"},
	}
	for _, tc := range cases {
		if got := deviceTypeForLog(tc.passed, tc.kind); got != tc.want {
			t.Fatalf("deviceTypeForLog(%q, %q)=%q want %q", tc.passed, tc.kind, got, tc.want)
		}
	}
}

func TestAnalysisUsedFallback(t *testing.T) {
	if !analysisUsedFallback("pipeline=hybrid|vision=gpt-4o(ok)|coach=gpt-4o(openai,fallback)", "ok") {
		t.Fatal("coach fallback")
	}
	if !analysisUsedFallback("pipeline=hybrid|vision=gpt-4o(unavailable)|coach=claude(anthropic)", "unavailable") {
		t.Fatal("vision unavailable")
	}
	if !analysisUsedFallback("", "no_openai_key") {
		t.Fatal("missing vision key")
	}
	if analysisUsedFallback("pipeline=hybrid|vision=gpt-4o(ok)|coach=claude(anthropic)", "ok") {
		t.Fatal("primary path is not a fallback")
	}
}

func TestClampLatency(t *testing.T) {
	if clampLatency(-5) != 0 || clampLatency(12) != 12 {
		t.Fatal("latency clamp")
	}
}

func assertNoPrivateLogText(t *testing.T, line string) {
	t.Helper()
	lower := strings.ToLower(line)
	for _, bad := range []string{
		"@",
		"http://",
		"https://",
		"uploads/",
		"r2.cloudflarestorage",
		"mozilla/",
		"user-agent",
		".jpg",
		"checks/",
		"bearer",
		"user@dadiary.vn",
		"secret.example",
	} {
		if strings.Contains(lower, bad) {
			t.Fatalf("log contains %q: %s", bad, line)
		}
	}
}
