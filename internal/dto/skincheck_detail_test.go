package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestOldSkinCheckResponseOmitsDetailFields(t *testing.T) {
	t.Parallel()
	checkID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	analysisID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	when := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

	scores := map[string]any{
		"overall":            0.62,
		"hydration":          0.4,
		"clarity":            0.7,
		"barrier":            0.55,
		"situation_analysis": "Má hơi đỏ.",
		"concern_alignment":  "Khớp với cảm nhận.",
		"photo_evidence":     "ok",
		"care_suggestions": []map[string]string{
			{"slot": "morning", "step": "Chống nắng", "why": "Má đang đỏ."},
		},
	}
	ss, err := json.Marshal(scores)
	if err != nil {
		t.Fatal(err)
	}
	check := &domain.SkinCheck{
		ID:         checkID,
		UserID:     userID,
		UserNote:   "má hơi căng",
		Conditions: mustRawJSON(t, []string{"dry"}),
		Symptoms:   mustRawJSON(t, []string{"itchy"}),
		Visibility: domain.CheckVisibilityPrivate,
		CheckDate:  when,
		CreatedAt:  when,
	}
	analysis := &domain.SkinAnalysis{
		ID:            analysisID,
		SkinCheckID:   checkID,
		Status:        domain.AnalysisStatusCompleted,
		ModelVersion:  "pipeline=hybrid|vision=gpt-4o(ok)|coach=claude(anthropic)",
		PromptVersion: 28,
		SummaryNotes:  "Mai chụp cùng góc.",
		Strengths:     mustRawJSON(t, []string{"Đã gửi ảnh"}),
		SkinScores:    ss,
		RoutineHints:  mustRawJSON(t, []string{"Sáng: chống nắng"}),
		AvoidOrPatch:  mustRawJSON(t, []string{"Đừng nặn"}),
		SafetyFlags: mustRawJSON(t, map[string]any{
			"reminders":  []string{"Đến gặp bác sĩ nếu sưng nhanh."},
			"disclaimer": "Không thay thế bác sĩ da liễu.",
		}),
	}
	res := NewCreateSkinCheckResponse(check, analysis, []string{"/uploads/checks/a.jpg"})
	got, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"check":{"id":"11111111-1111-1111-1111-111111111111","user_id":"22222222-2222-2222-2222-222222222222","user_note":"má hơi căng","conditions":["dry"],"symptoms":["itchy"],"visibility":"private","check_date":"2026-10-01","created_at":"2026-10-01T08:00:00Z"},"analysis":{"id":"33333333-3333-3333-3333-333333333333","skin_check_id":"11111111-1111-1111-1111-111111111111","status":"completed","model_version":"pipeline=hybrid|vision=gpt-4o(ok)|coach=claude(anthropic)","prompt_version":28,"coach":{"summary_notes":"Mai chụp cùng góc.","strengths":["Đã gửi ảnh"],"situation_summary":"Má hơi đỏ.","concern_alignment":"Khớp với cảm nhận.","skin_score_gauges":{"overall":0.62,"hydration":0.4,"clarity":0.7,"barrier":0.55},"care_suggestions":[{"slot":"morning","step":"Chống nắng","why":"Má đang đỏ."}],"routine_hints":["Sáng: chống nắng"],"avoid_or_patch":["Đừng nặn"],"safety_reminders":["Đến gặp bác sĩ nếu sưng nhanh."],"medical_disclaimer":"Không thay thế bác sĩ da liễu.","photo_evidence":"ok"}},"image_urls":["/uploads/checks/a.jpg"]}`
	if string(got) != want {
		t.Fatalf("old response\n got %s\nwant %s", got, want)
	}
	for _, absent := range []string{`"zone_notes"`, `"skin_score_notes"`, `"confidence"`, `"needs_more_info"`, `"clarify_questions"`, `"photo_meta"`, `"skin_context"`, `"visible_observations"`} {
		if json.Valid(got) && containsJSONKey(t, got, absent) {
			t.Fatalf("old response includes %s", absent)
		}
	}
}

func TestNewSkinCheckResponseMapsDetailWithoutChangingGauges(t *testing.T) {
	t.Parallel()
	checkID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	when := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	base := map[string]any{
		"overall":   0.62,
		"hydration": 0.4,
		"clarity":   0.7,
		"barrier":   0.55,
	}
	withNotes := map[string]any{
		"overall":   0.62,
		"hydration": 0.4,
		"clarity":   0.7,
		"barrier":   0.55,
		"zone_notes": []map[string]string{
			{"zone": "left_cheek", "note": "Má trông giống mụn ẩn.", "severity": "mild"},
		},
		"skin_score_notes": map[string]string{
			"overall":   "Tổng thể hơi thấp vì má đỏ.",
			"hydration": "Má hơi bong vảy nên độ ẩm thấp.",
			"clarity":   "Vài đốm thâm nên độ đều màu vừa.",
			"barrier":   "Hơi căng nên lớp bảo vệ chưa ổn.",
		},
		"confidence":               "medium",
		"needs_more_info":          true,
		"clarify_questions":        []string{"Sờ vào thấy cứng như hạt cát, hay mềm?"},
		"visible_observations":     []string{"má: nốt nhỏ"},
		"vision_zone_observations": []map[string]string{{"zone": "left_cheek", "cue": "nốt nhỏ", "severity": "mild"}},
		"situation_analysis":       "Má hơi đỏ.",
		"photo_evidence":           "ok",
	}
	oldGauges := gaugesOf(t, checkID, userID, when, base)
	newDetail := detailOf(t, checkID, userID, when, withNotes)
	if newDetail.SkinScoreGauges == nil || oldGauges == nil {
		t.Fatal("gauges missing")
	}
	if *newDetail.SkinScoreGauges.Overall != *oldGauges.Overall ||
		*newDetail.SkinScoreGauges.Hydration != *oldGauges.Hydration ||
		*newDetail.SkinScoreGauges.Clarity != *oldGauges.Clarity ||
		*newDetail.SkinScoreGauges.Barrier != *oldGauges.Barrier {
		t.Fatalf("gauges changed: old=%+v new=%+v", oldGauges, newDetail.SkinScoreGauges)
	}
	if len(newDetail.ZoneNotes) != 1 || newDetail.ZoneNotes[0].Zone != "left_cheek" || newDetail.ZoneNotes[0].Severity != "mild" {
		t.Fatalf("zone notes %#v", newDetail.ZoneNotes)
	}
	if newDetail.SkinScoreNotes == nil || newDetail.SkinScoreNotes.Overall == "" || newDetail.SkinScoreNotes.Hydration == "" || newDetail.SkinScoreNotes.Clarity == "" || newDetail.SkinScoreNotes.Barrier == "" {
		t.Fatalf("score notes %#v", newDetail.SkinScoreNotes)
	}
	if newDetail.Confidence != "medium" || !newDetail.NeedsMoreInfo || len(newDetail.ClarifyQuestions) != 1 {
		t.Fatalf("confidence payload %+v", newDetail)
	}
	raw, err := json.Marshal(newDetail)
	if err != nil {
		t.Fatal(err)
	}
	var pub map[string]any
	if err := json.Unmarshal(raw, &pub); err != nil {
		t.Fatal(err)
	}
	if _, ok := pub["visible_observations"]; ok {
		t.Fatal("visible_observations must stay off the public coach payload")
	}
	if _, ok := pub["vision_zone_observations"]; ok {
		t.Fatal("vision_zone_observations must stay off the public coach payload")
	}

	photo := mustRawJSON(t, map[string]any{
		"images": []map[string]any{{"index": 0, "kind": "closeup", "zone": "left_cheek"}},
		"skin_context": map[string]string{
			"firmness": "firm", "duration": "months", "pain": "none", "extra": "không đổi",
		},
	})
	check := &domain.SkinCheck{
		ID: checkID, UserID: userID, Visibility: domain.CheckVisibilityPrivate,
		CheckDate: when, CreatedAt: when, PhotoContext: photo,
	}
	res := NewCreateSkinCheckResponse(check, &domain.SkinAnalysis{
		ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), SkinCheckID: checkID,
		Status: domain.AnalysisStatusCompleted, SkinScores: mustRawJSON(t, withNotes),
	}, nil)
	if len(res.Check.PhotoMeta) != 1 || res.Check.PhotoMeta[0].Zone != "left_cheek" || res.Check.PhotoMeta[0].Kind != "closeup" {
		t.Fatalf("photo_meta %#v", res.Check.PhotoMeta)
	}
	if res.Check.SkinContext == nil || res.Check.SkinContext.Firmness != "firm" || res.Check.SkinContext.Extra != "không đổi" {
		t.Fatalf("skin_context %#v", res.Check.SkinContext)
	}
}

func TestProgressIgnoresDetailTextUnderScoreNotes(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	numbers := map[string]any{"overall": 0.5, "hydration": 0.25, "clarity": 0.8, "barrier": 0.6}
	withText := map[string]any{
		"overall": 0.5, "hydration": 0.25, "clarity": 0.8, "barrier": 0.6,
		"skin_score_notes": map[string]string{
			"overall": "Tổng thể.", "hydration": "Khô.", "clarity": "Đều.", "barrier": "Căng.",
		},
		"zone_notes": []map[string]string{{"zone": "chin", "note": "Cằm trông giống mụn ẩn."}},
	}
	plain := progressRow(t, when, numbers)
	noted := progressRow(t, when, withText)
	plainSum := computeProgressSummary([]domain.SkinCheck{plain})
	notedSum := computeProgressSummary([]domain.SkinCheck{noted})
	if plainSum.Buckets[0].OverallAvg == nil || notedSum.Buckets[0].OverallAvg == nil {
		t.Fatal("missing average")
	}
	if *plainSum.Buckets[0].OverallAvg != *notedSum.Buckets[0].OverallAvg ||
		*plainSum.Buckets[0].HydrationAvg != *notedSum.Buckets[0].HydrationAvg ||
		*plainSum.Buckets[0].ClarityAvg != *notedSum.Buckets[0].ClarityAvg ||
		*plainSum.Buckets[0].BarrierAvg != *notedSum.Buckets[0].BarrierAvg {
		t.Fatalf("averages moved\n plain %+v\n noted %+v", plainSum.Buckets[0], notedSum.Buckets[0])
	}
	timeline := NewProgressTimelineResponse([]domain.SkinCheck{noted}, 30, "/uploads")
	raw, err := json.Marshal(timeline.Entries[0])
	if err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "check_date", "created_at", "image_urls", "status", "gauges", "snippet"} {
		if _, ok := entry[key]; !ok {
			t.Fatalf("progress entry missing %s: %s", key, raw)
		}
	}
	if _, ok := entry["zone_notes"]; ok {
		t.Fatal("progress entry grew zone_notes")
	}
	if _, ok := entry["skin_score_notes"]; ok {
		t.Fatal("progress entry grew skin_score_notes")
	}
	gauges, _ := entry["gauges"].(map[string]any)
	if gauges["overall"] != 0.5 || gauges["hydration"] != 0.25 {
		t.Fatalf("gauges %#v", gauges)
	}
}

func TestWebCheckFieldsStayOnGetPayload(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	check := &domain.SkinCheck{
		ID: uuid.New(), UserID: uuid.New(),
		UserNote: "note", Conditions: mustRawJSON(t, []string{"oily"}),
		Symptoms:   mustRawJSON(t, []string{"sting"}),
		Visibility: domain.CheckVisibilityPrivate, CheckDate: when, CreatedAt: when,
	}
	analysis := &domain.SkinAnalysis{
		ID: uuid.New(), SkinCheckID: check.ID, Status: domain.AnalysisStatusCompleted,
		ModelVersion: "pipeline=hybrid|vision=gpt-4o(ok)|coach=claude(anthropic)", PromptVersion: 29,
		SummaryNotes: "Tóm tắt.",
		SkinScores: mustRawJSON(t, map[string]any{
			"overall": 0.5, "hydration": 0.5, "clarity": 0.5, "barrier": 0.5,
			"situation_analysis": "Hôm nay.", "concern_alignment": "Khớp.",
			"photo_evidence": "limited", "photo_limited": true, "photo_limited_note": "hơi mờ",
			"zone_notes": []map[string]string{{"zone": "chin", "note": "Cằm trông giống mụn ẩn."}},
		}),
		Strengths:    mustRawJSON(t, []string{"Ổn"}),
		Improvements: mustRawJSON(t, []map[string]string{{"tip": "Sáng: dưỡng", "why": "Khô"}}),
		RoutineHints: mustRawJSON(t, []string{"Sáng: dưỡng ẩm"}),
		AvoidOrPatch: mustRawJSON(t, []string{"Thử vùng nhỏ"}),
		SafetyFlags:  mustRawJSON(t, map[string]any{"reminders": []string{"SPF"}, "disclaimer": "Không phải khám."}),
	}
	res := NewCreateSkinCheckResponse(check, analysis, []string{"/uploads/a.jpg"})
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	chk := body["check"].(map[string]any)
	for _, key := range []string{"id", "user_note", "conditions", "symptoms"} {
		if _, ok := chk[key]; !ok {
			t.Fatalf("check missing %s", key)
		}
	}
	if _, ok := body["image_urls"]; !ok {
		t.Fatal("missing image_urls")
	}
	an := body["analysis"].(map[string]any)
	for _, key := range []string{"id", "status", "model_version", "prompt_version", "coach"} {
		if _, ok := an[key]; !ok {
			t.Fatalf("analysis missing %s", key)
		}
	}
	coach := an["coach"].(map[string]any)
	for _, key := range []string{
		"situation_summary", "concern_alignment", "skin_score_gauges", "care_suggestions",
		"improvements", "routine_hints", "avoid_or_patch", "safety_reminders",
		"medical_disclaimer", "photo_evidence", "photo_limited", "summary_notes",
	} {
		if _, ok := coach[key]; !ok {
			t.Fatalf("coach missing web field %s in %s", key, raw)
		}
	}
	gauges := coach["skin_score_gauges"].(map[string]any)
	for _, key := range []string{"overall", "hydration", "clarity", "barrier"} {
		if _, ok := gauges[key].(float64); !ok {
			t.Fatalf("gauge %s is %T", key, gauges[key])
		}
	}
}

func gaugesOf(t *testing.T, checkID, userID uuid.UUID, when time.Time, scores map[string]any) *SkinCoachScoreGauges {
	t.Helper()
	d := detailOf(t, checkID, userID, when, scores)
	return d.SkinScoreGauges
}

func detailOf(t *testing.T, checkID, userID uuid.UUID, when time.Time, scores map[string]any) *SkinCoachDetail {
	t.Helper()
	check := &domain.SkinCheck{
		ID: checkID, UserID: userID, Visibility: domain.CheckVisibilityPrivate,
		CheckDate: when, CreatedAt: when,
	}
	a := &domain.SkinAnalysis{
		ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), SkinCheckID: checkID,
		Status: domain.AnalysisStatusCompleted, SkinScores: mustRawJSON(t, scores),
	}
	res := NewCreateSkinCheckResponse(check, a, nil)
	if res.Analysis.Coach == nil {
		t.Fatal("nil coach")
	}
	return res.Analysis.Coach
}

func progressRow(t *testing.T, when time.Time, scores map[string]any) domain.SkinCheck {
	t.Helper()
	id := uuid.New()
	return domain.SkinCheck{
		ID: id, UserID: uuid.New(), CheckDate: when, CreatedAt: when,
		Visibility: domain.CheckVisibilityPrivate,
		ImageURLs:  mustRawJSON(t, []string{"checks/a.jpg"}),
		Analysis: &domain.SkinAnalysis{
			ID: uuid.New(), SkinCheckID: id, Status: domain.AnalysisStatusCompleted,
			SummaryNotes: "Một dòng.", SkinScores: mustRawJSON(t, scores),
		},
	}
}

func containsJSONKey(t *testing.T, raw []byte, quoted string) bool {
	t.Helper()
	return json.Valid(raw) && contains(string(raw), quoted)
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > 0 && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
