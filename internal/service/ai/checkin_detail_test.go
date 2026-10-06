package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

func TestNormalizeCheckInDetail_LenientAndDropsBadNotes(t *testing.T) {
	t.Parallel()
	vision := `{"visible_observations":["má: nốt nhỏ"],"zone_observations":[{"zone":"left_cheek","cue":"nốt nhỏ màu da nổi cao","severity":"mild"},{"zone":"chin","cue":"cằm hơi đỏ","severity":"moderate"}]}`
	photo := photoContextJSON(t, []dto.PhotoMetaImage{{Index: 0, Kind: "closeup", Zone: "chin"}}, nil)

	// Wrong types never fail the surrounding parse.
	parsed, err := parseCoachStructuredOutput(`{"score":0.4,"zone_notes":"nope","skin_score_notes":["nope"],"summary_notes":"ok"}`, "test")
	if err != nil {
		t.Fatal(err)
	}
	notes, scores := NormalizeCheckInDetail(parsed.ZoneNotesRaw, parsed.SkinScoreNotesRaw, vision, photo, PhotoEvidenceOK, "vi")
	if scores != nil {
		t.Fatalf("bad score notes should drop, got %#v", scores)
	}
	// zone_notes was not an array, so the coach notes are empty and the ok photo
	// falls back to vision zones.
	if len(notes) != 2 {
		t.Fatalf("fallback notes %#v", notes)
	}
	for _, n := range notes {
		if !strings.Contains(n.Note, "Trông giống") {
			t.Fatalf("fallback must say trông giống: %q", n.Note)
		}
	}

	rawNotes := mustRawJSON(t, []any{
		map[string]any{"zone": "left_cheek", "note": "Má trông giống mụn ẩn.", "severity": "mild"},
		map[string]any{"zone": "forehead", "note": "Trán trông giống mụn ẩn."},
		map[string]any{"zone": "temple", "note": "Thái dương trông giống mụn ẩn."},
		map[string]any{"zone": "left_cheek", "note": "Má trông giống eczema và câu này phải bị bỏ cả đoạn, không được cắt còn lại.", "severity": "severe"},
		map[string]any{"zone": "chin", "note": "Cằm hơi đỏ vài nốt.", "severity": 3},
		map[string]any{"zone": "chin", "note": 12, "severity": "mild"},
		map[string]any{"zone": "left_cheek", "note": "Má của mày trông giống mụn ẩn, không phải chẩn đoán.", "severity": "nope"},
	})
	rawScores := mustRawJSON(t, map[string]any{
		"overall":   "Tổng thể hơi thấp vì má đỏ.",
		"hydration": "Má hơi bong vảy nên độ ẩm thấp.",
		"clarity":   4,
		"barrier":   "Độ ẩm thấp vì eczema nên câu này bỏ cả câu.",
		"extra":     "ignored",
	})
	notes, scores = NormalizeCheckInDetail(rawNotes, rawScores, vision, photo, PhotoEvidenceOK, "vi")
	if scores == nil || scores.Overall == "" || scores.Hydration == "" || scores.Clarity != "" || scores.Barrier != "" {
		t.Fatalf("score notes %#v", scores)
	}
	if strings.Contains(scores.Barrier, "eczema") || strings.Contains(scores.Hydration, "eczema") {
		t.Fatal("disease sentence must be dropped whole, not trimmed")
	}
	joined := joinNotes(notes)
	if strings.Contains(joined, "eczema") || strings.Contains(joined, "bỏ cả đoạn") {
		t.Fatalf("disease note was truncated instead of dropped: %s", joined)
	}
	if strings.Contains(joined, "forehead") || strings.Contains(joined, "Trán") || strings.Contains(joined, "Thái dương") {
		t.Fatalf("invented zone kept: %s", joined)
	}
	if strings.Contains(joined, "Cằm hơi đỏ") {
		t.Fatal("note without trông giống must be dropped whole")
	}
	foundCheek := false
	for _, n := range notes {
		if n.Zone == "left_cheek" && n.Note == "Má trông giống mụn ẩn." && n.Severity == "mild" {
			foundCheek = true
		}
		if n.Severity == "severe" || n.Severity == "nope" {
			t.Fatalf("bad severity kept on %#v", n)
		}
	}
	if !foundCheek {
		t.Fatalf("good cheek note missing: %#v", notes)
	}
}

func TestNormalizeCheckInDetail_CapsAtFiveWithoutCuttingText(t *testing.T) {
	t.Parallel()
	vision := `{"zone_observations":[{"zone":"left_cheek","cue":"a","severity":"mild"},{"zone":"right_cheek","cue":"b","severity":"mild"},{"zone":"chin","cue":"c","severity":"mild"},{"zone":"forehead","cue":"d","severity":"mild"},{"zone":"nose","cue":"e","severity":"mild"},{"zone":"jawline","cue":"f","severity":"mild"}]}`
	items := []any{}
	full := []string{
		"Má trái trông giống mụn ẩn và câu này dài nguyên vẹn.",
		"Má phải trông giống mụn ẩn và câu này dài nguyên vẹn.",
		"Cằm trông giống mụn ẩn và câu này dài nguyên vẹn.",
		"Trán trông giống mụn ẩn và câu này dài nguyên vẹn.",
		"Mũi trông giống mụn ẩn và câu này dài nguyên vẹn.",
		"Hàm trông giống mụn ẩn và câu này dài nguyên vẹn không được cắt cụt.",
	}
	zones := []string{"left_cheek", "right_cheek", "chin", "forehead", "nose", "jawline"}
	for i := range full {
		items = append(items, map[string]string{"zone": zones[i], "note": full[i], "severity": "mild"})
	}
	notes, _ := NormalizeCheckInDetail(mustRawJSON(t, items), nil, vision, nil, PhotoEvidenceOK, "vi")
	if len(notes) != 5 {
		t.Fatalf("len %d", len(notes))
	}
	if notes[4].Note != full[4] {
		t.Fatalf("fifth note was cut: %q", notes[4].Note)
	}
	if strings.Contains(joinNotes(notes), "không được cắt cụt") {
		t.Fatal("sixth note should be dropped whole")
	}
}

func TestNormalizeCheckInDetail_SkipHasNoZoneNotes(t *testing.T) {
	t.Parallel()
	vision := `{"zone_observations":[{"zone":"chin","cue":"nốt nhỏ","severity":"mild"}]}`
	raw := mustRawJSON(t, []map[string]string{{"zone": "chin", "note": "Cằm trông giống mụn ẩn.", "severity": "mild"}})
	notes, _ := NormalizeCheckInDetail(raw, nil, vision, nil, PhotoEvidenceSkip, "vi")
	if len(notes) != 0 {
		t.Fatalf("skip should clear zone notes, got %#v", notes)
	}
}

func TestApplyCheckInDetail_AndroidVoiceAndStoredVision(t *testing.T) {
	t.Parallel()
	vision := `{"visible_observations":["má của mày: nốt nhỏ"],"zone_observations":[{"zone":"left_cheek","cue":"nốt nhỏ màu da","severity":"mild"}]}`
	labels := map[string]any{"overall": 0.5, "hydration": 0.4}
	parsed := &CoachStructuredOutput{
		ZoneNotesRaw: mustRawJSON(t, []map[string]string{{
			"zone": "left_cheek", "note": "Má của mày trông giống mụn ẩn.", "severity": "mild",
		}}),
		SkinScoreNotesRaw: mustRawJSON(t, map[string]string{
			"overall": "Tổng thể của mày hơi thấp.", "hydration": "Độ ẩm của mày thấp.",
			"clarity": "Màu khá đều.", "barrier": "Lớp bảo vệ ổn.",
		}),
	}
	ApplyCheckInDetail(labels, parsed, vision, nil, CheckInPhotoEvidence{Kind: PhotoEvidenceOK}, domain.RefreshClientAndroid, "vi")
	notes := labels["zone_notes"].([]dto.CoachZoneNote)
	if strings.Contains(notes[0].Note, "mày") || !strings.Contains(notes[0].Note, "bạn") {
		t.Fatalf("android note %q", notes[0].Note)
	}
	scores := labels["skin_score_notes"].(*dto.SkinCoachScoreNotes)
	if strings.Contains(scores.Overall, "mày") || !strings.Contains(scores.Overall, "bạn") {
		t.Fatalf("android score %q", scores.Overall)
	}
	if labels["overall"] != 0.5 || labels["hydration"] != 0.4 {
		t.Fatalf("numeric gauges changed: %#v", labels)
	}
	if _, ok := labels["visible_observations"].([]string); !ok {
		t.Fatal("visible_observations was dropped")
	}
	if _, ok := labels["vision_zone_observations"].([]visionZone); !ok {
		t.Fatalf("vision zones %#v", labels["vision_zone_observations"])
	}

	web := map[string]any{}
	ApplyCheckInDetail(web, parsed, vision, nil, CheckInPhotoEvidence{Kind: PhotoEvidenceOK}, domain.RefreshClientWeb, "vi")
	webNotes := web["zone_notes"].([]dto.CoachZoneNote)
	if !strings.Contains(webNotes[0].Note, "mày") {
		t.Fatalf("web voice changed: %q", webNotes[0].Note)
	}
}

func TestCheckInDetailBlock_PhotoCheckInOnly(t *testing.T) {
	t.Parallel()
	if strings.Contains(CheckInDetailJSONFields, "tao") || strings.Contains(CheckInDetailJSONFields, "mày") {
		t.Fatal("detail schema must not pick a voice")
	}
	if !strings.Contains(CheckInDetailJSONFields, `"overall"`) || !strings.Contains(CheckInDetailJSONFields, "trông giống") {
		t.Fatal("detail schema missing overall or trông giống")
	}
	if strings.Contains(CoachOutputJSONSchemaBlock, `"zone_notes"`) || strings.Contains(coachOutputJSONSchemaBlockAndroid, `"zone_notes"`) {
		t.Fatal("shared coach schema must not carry the photo-only block")
	}
	msg := buildSkinCheckCoachUserMessage(&domain.SkinCheck{}, nil, "", `{"visible_observations":["má"]}`, "ok", "USER_INTERFACE_LOCALE: vi")
	if !strings.Contains(msg, `"zone_notes"`) || !strings.Contains(msg, `"skin_score_notes"`) {
		t.Fatal("photo check-in prompt missing detail keys")
	}
	_, daily := buildDailyFeedbackPromptForClient("Hôm nay da ổn", "beginner", "")
	if strings.Contains(daily, `"zone_notes"`) || strings.Contains(daily, `"skin_score_notes"`) {
		t.Fatal("daily feedback must not pay for check-in detail keys")
	}
	_, androidDaily := buildDailyFeedbackPromptForClient("Hôm nay da ổn", "beginner", domain.RefreshClientAndroid)
	if strings.Contains(androidDaily, `"zone_notes"`) {
		t.Fatal("android daily feedback picked up zone_notes")
	}
}

func TestSanitizeCheckInVisionJSON_KeepsZoneObservations(t *testing.T) {
	t.Parallel()
	raw := `{"photo_assessment":{"lighting":"ok","angle_clarity":"ok","limitations":""},` +
		`"visible_observations":["má: nhiều nốt màu da nổi cao, trông giống mụn thịt"],` +
		`"zone_observations":[{"zone":"left_cheek","cue":"má: trông giống mụn thịt","severity":"moderate"},{"zone":"chin","cue":"cằm hơi bóng","severity":"mild"}],` +
		`"texture_and_oil_cues":"bề mặt hơi gồ","redness_or_discoloration_cues":"không đỏ","uncertainty_note":""}`
	out, changed := SanitizeCheckInVisionJSON(raw, "vi")
	if !changed {
		t.Fatal("expected a rewrite")
	}
	for _, must := range []string{`"zone":"left_cheek"`, `"severity":"moderate"`, "cằm hơi bóng", `"zone":"chin"`, "bề mặt hơi gồ"} {
		if !strings.Contains(out, must) {
			t.Fatalf("sanitizing dropped %q from %s", must, out)
		}
	}
	if strings.Contains(out, "mụn thịt") {
		t.Fatalf("zone cue still says mụn thịt: %s", out)
	}
}

func photoContextJSON(t *testing.T, images []dto.PhotoMetaImage, skin *dto.SkinContextInput) json.RawMessage {
	t.Helper()
	return mustRawJSON(t, dto.StoredPhotoContext{Images: images, SkinContext: skin})
}

func mustRawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func joinNotes(notes []dto.CoachZoneNote) string {
	parts := make([]string, 0, len(notes))
	for _, n := range notes {
		parts = append(parts, n.Zone+" "+n.Note)
	}
	return strings.Join(parts, " | ")
}
