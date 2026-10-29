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
		CheckID:       id,
		Confidence:    poisonedNote,
		DeviceType:    poisonedAgent,
		PhotoPresent:  true,
		AILatencyMs:   842,
		Outcome:       "completed see https://secret.example/token",
		Fallback:      true,
		PhotoEvidence: "limited " + poisonedNote,
		LowReasonPath: "photo " + poisonedNote,
		PhotoNoteTag:  "ảnh hơi mờ " + poisonedNote,
		ClarifyCount:  -8,
		ClarifyKind:   "retake_tips " + poisonedAgent,
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
	wantKeys := []string{
		"check_id", "confidence", "device_type", "photo_present", "ai_latency_ms", "outcome", "fallback",
		"photo_evidence", "low_reason_path", "photo_note_tag", "clarify_count", "clarify_kind",
	}
	if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("keys %v", keys)
	}
	if args[1] != id.String() || args[3] != "unknown" || args[5] != "unknown" || args[7] != true || args[9] != int64(842) || args[11] != "unknown" || args[13] != true {
		t.Fatalf("clamped args %#v", args)
	}
	if args[15] != "unknown" || args[17] != "none" || args[19] != "none" || args[21] != 0 || args[23] != "none" {
		t.Fatalf("clamped reason args %#v", args)
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
		CheckID:       id,
		Confidence:    " LOW ",
		DeviceType:    " web-android ",
		PhotoPresent:  true,
		AILatencyMs:   1500,
		Outcome:       "completed",
		Fallback:      false,
		PhotoEvidence: " LIMITED ",
		LowReasonPath: " PHOTO ",
		PhotoNoteTag:  " BLUR ",
		ClarifyCount:  3,
		ClarifyKind:   " RETAKE_TIPS ",
	})
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("skin_check_ai_result", args...)
	line := buf.String()
	assertNoPrivateLogText(t, line)
	for _, want := range []string{
		`"confidence":"low"`,
		`"device_type":"web-android"`,
		`"photo_present":true`,
		`"ai_latency_ms":1500`,
		`"outcome":"completed"`,
		`"fallback":false`,
		`"photo_evidence":"limited"`,
		`"low_reason_path":"photo"`,
		`"photo_note_tag":"blur"`,
		`"clarify_count":3`,
		`"clarify_kind":"retake_tips"`,
		id.String(),
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %s in %s", want, line)
		}
	}
}

func TestSkinCheckAIResultArgs_DeviceTypeOnSameLine(t *testing.T) {
	id := uuid.MustParse("bbbbbbbb-cccc-dddd-eeee-ffffffffffff")
	for _, device := range []string{"web-android", "web-ios", "android-app", "unknown", "web-desktop"} {
		var buf bytes.Buffer
		args := skinCheckAIResultArgs(aiResultLog{
			CheckID:      id,
			Confidence:   "high",
			DeviceType:   device,
			PhotoPresent: true,
			Outcome:      "completed",
		})
		slog.New(slog.NewJSONHandler(&buf, nil)).Info("skin_check_ai_result", args...)
		line := buf.String()
		assertNoPrivateLogText(t, line)
		if !strings.Contains(line, `"device_type":"`+device+`"`) || !strings.Contains(line, "skin_check_ai_result") {
			t.Fatalf("device %s missing from %s", device, line)
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

func TestLowReasonPathAndClarifyKind(t *testing.T) {
	cases := []struct {
		evidence   string
		count      int
		kind, path string
	}{
		{"limited", 3, "retake_tips", "photo"},
		{"limited", 0, "none", "photo"},
		{"skip", 0, "none", "photo"},
		{"skip", 2, "none", "photo"},
		{"ok", 2, "touch_questions", "clarify_pending"},
		{"ok", 0, "none", "none"},
		{"", 4, "none", "none"},
	}
	for _, tc := range cases {
		kind := clarifyKindForLog(tc.evidence, tc.count)
		path := lowReasonPath(tc.evidence, kind)
		if kind != tc.kind || path != tc.path {
			t.Fatalf("evidence %q count %d: kind %q path %q, want kind %q path %q", tc.evidence, tc.count, kind, path, tc.kind, tc.path)
		}
	}
}

func TestApplyLowConfidenceLogFields_DoesNotLogNoteText(t *testing.T) {
	id := uuid.MustParse("cccccccc-dddd-eeee-ffff-000000000001")
	vision := `{
		"photo_assessment":{"lighting":"dark shadow crop blur","angle_clarity":"ok","limitations":"ảnh hơi mờ NOTETOKEN user@dadiary.vn https://cdn.example/uploads/checks/face.jpg"},
		"visible_observations":["OBSERVATIONTOKEN má user@secret.example"],
		"uncertainty_note":"https://secret.example/token"
	}`
	labels := map[string]any{
		"confidence":         "low",
		"photo_limited_note": "LIMITEDNOTETOKEN ảnh hơi mờ user@dadiary.vn https://cdn.example/a.jpg",
		"clarify_questions": []string{
			"",
			"  ",
			"QUESTIONTOKEN Chụp lại gần cửa sổ. https://cdn.example/uploads/checks/face.jpg",
			"Sờ vào thấy cứng như hạt cát?",
		},
	}
	entry := aiResultLog{
		CheckID:      id,
		Confidence:   "low",
		DeviceType:   "web-android",
		PhotoPresent: true,
		AILatencyMs:  1500,
		Outcome:      "completed",
		Fallback:     false,
	}
	applyLowConfidenceLogFields(&entry, "ok", vision, labels)
	args := skinCheckAIResultArgs(entry)
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("skin_check_ai_result", args...)
	line := buf.String()
	assertNoPrivateLogText(t, line)
	for _, leaked := range []string{"NOTETOKEN", "OBSERVATIONTOKEN", "LIMITEDNOTETOKEN", "QUESTIONTOKEN", "hạt cát", "cửa sổ", "mờ", "limitations"} {
		if strings.Contains(line, leaked) {
			t.Fatalf("log contains %q: %s", leaked, line)
		}
	}
	for _, want := range []string{
		`"confidence":"low"`,
		`"device_type":"web-android"`,
		`"photo_evidence":"limited"`,
		`"low_reason_path":"photo"`,
		`"photo_note_tag":"blur"`,
		`"clarify_count":2`,
		`"clarify_kind":"retake_tips"`,
		id.String(),
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %s in %s", want, line)
		}
	}

	pending := aiResultLog{CheckID: id, Confidence: "low", DeviceType: "web-ios", PhotoPresent: true, Outcome: "completed"}
	okVision := `{"photo_assessment":{"limitations":""},"uncertainty_note":"","visible_observations":["má: vài nốt"]}`
	applyLowConfidenceLogFields(&pending, "ok", okVision, map[string]any{
		"clarify_questions": []string{"Sờ vào thấy cứng như hạt cát, hay mềm?", "Nốt này mới nổi vài ngày hay đã vài tháng?"},
	})
	if pending.PhotoEvidence != "ok" || pending.LowReasonPath != "clarify_pending" || pending.ClarifyKind != "touch_questions" || pending.ClarifyCount != 2 || pending.PhotoNoteTag != "none" {
		t.Fatalf("clarify pending: %+v", pending)
	}

	other := aiResultLog{PhotoPresent: true}
	applyLowConfidenceLogFields(&other, "ok", `{"photo_assessment":{"limitations":"góc chụp hẹp NOTETOKEN"},"uncertainty_note":""}`, nil)
	if other.PhotoEvidence != "ok" || other.PhotoNoteTag != "other" || other.LowReasonPath != "none" || other.ClarifyKind != "none" || other.ClarifyCount != 0 {
		t.Fatalf("non-keyword note: %+v", other)
	}

	failed := aiResultLog{PhotoPresent: true}
	applyLowConfidenceLogFields(&failed, "unavailable", "", nil)
	if failed.PhotoEvidence != "limited" || failed.PhotoNoteTag != "none" || failed.LowReasonPath != "photo" || failed.ClarifyCount != 0 || failed.ClarifyKind != "none" {
		t.Fatalf("vision unavailable: %+v", failed)
	}

	skipped := aiResultLog{PhotoPresent: true}
	applyLowConfidenceLogFields(&skipped, "skipped_no_photo", "", map[string]any{
		"clarify_questions": []any{"should not be read user@x.com"},
	})
	if skipped.PhotoEvidence != "skip" || skipped.LowReasonPath != "photo" || skipped.ClarifyCount != 0 || skipped.ClarifyKind != "none" {
		t.Fatalf("skip evidence: %+v", skipped)
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
