package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestSanitizePublicCopy_StripsTechnicalTokens(t *testing.T) {
	cases := []struct{ in, want string }{
		{"score 0.78 vì da vẫn khỏe mạnh", "Da vẫn khỏe mạnh"},
		{"score 0,78 vì da vẫn khỏe mạnh", "Da vẫn khỏe mạnh"},
		{"Score 0.78: da vẫn khỏe", "Da vẫn khỏe"},
		{"situation_analysis: da đỏ", "Da đỏ"},
		{"skin_score_notes da khỏe", "Da khỏe"},
		{"care_suggestions không khớp", "Không khớp"},
		{"situation_analysis, zone_notes", ""},
		{"Mày ghi score 0.78, tao thấy da ổn", "Mày ghi, tao thấy da ổn"},
		{"Đm da mày hôm nay", "Đm da mày hôm nay"},
		{"kẻ mày, vùng mày, lông mày", "kẻ mày, vùng mày, lông mày"},
		{"Có 2–3 nốt ở má", "Có 2–3 nốt ở má"},
		{"Sáng: SPF50 mỗi ngày", "Sáng: SPF50 mỗi ngày"},
		{"ngày 7 tái khám", "ngày 7 tái khám"},
		{"tăng 50%", "tăng 50%"},
		{"khoảng 0.5%", "khoảng 0.5%"},
		{"mày 1,5.", "mày 1,5."},
		{"độ 10.5", "độ 10.5"},
		{"https://ex.com/skin_score_notes?score=1", "https://ex.com/skin_score_notes?score=1"},
		{"score 0.78", ""},
		{"Má trông giống mụn ẩn, score 0.78.", "Má trông giống mụn ẩn."},
		{"Da vẫn khỏe mạnh", "Da vẫn khỏe mạnh"},
		{"Lấy 0,5 ml serum", "Lấy 0,5 ml serum"},
		{"BHA 0.5–2%", "BHA 0.5–2%"},
		{"BHA 0.5-2%", "BHA 0.5-2%"},
		{"BHA 0.5 đến 2%", "BHA 0.5 đến 2%"},
		{"Retinol 0.025 hoặc 0.05", "Retinol 0.025 hoặc 0.05"},
		{"Gel 0.5g", "Gel 0.5g"},
		{"Bôi 1,0 lớp", "Bôi 1,0 lớp"},
		{"hydration: 0.78 vì da khô", "Da khô"},
		{"Hydration: 0.78 vì da khô", "Da khô"},
		{"overall: 0.78", ""},
		{"Retinol: 0.025", "Retinol: 0.025"},
		{"Vitamin C: 0.025", "Vitamin C: 0.025"},
		{"BHA: 0.5", "BHA: 0.5"},
	}
	for _, tc := range cases {
		got := SanitizePublicCopy(tc.in)
		if got != tc.want {
			t.Errorf("SanitizePublicCopy(%q)=%q want %q", tc.in, got, tc.want)
		}
		if again := SanitizePublicCopy(got); again != got {
			t.Errorf("not stable: %q -> %q", got, again)
		}
	}
}

func TestSanitizePublicCopy_KeepsPronounsAndProfanity(t *testing.T) {
	in := "Đm da mày hôm nay vl, tao thấy."
	if got := SanitizePublicCopy(in); got != in {
		t.Fatalf("filter rewrote voice: %q", got)
	}
}

func TestSanitizePublicCoachOutput_NotesAndQuestions(t *testing.T) {
	out := &CoachStructuredOutput{
		SummaryNotes:      "score 0.78 vì da vẫn khỏe mạnh",
		SituationAnalysis: "Mày thấy situation_analysis da đỏ",
		RoutineHints:      []string{"Sáng: SPF50", "Tối: ngày 7"},
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{{Tip: "Có 2–3 nốt", Why: "score 0,78 vì má đỏ"}},
		ZoneNotesRaw: mustPublicJSON(t, []map[string]string{{
			"zone": "left_cheek", "note": "Má trông giống mụn ẩn, score 0.78.", "severity": "mild",
		}}),
		SkinScoreNotesRaw: mustPublicJSON(t, map[string]string{
			"overall": "score 0.78 vì da vẫn khỏe mạnh",
			"clarity": "Có 2–3 nốt, SPF50, ngày 7",
		}),
	}
	if !SanitizePublicCoachOutput(out) {
		t.Fatal("expected a change")
	}
	if out.SummaryNotes != "Da vẫn khỏe mạnh" {
		t.Fatalf("summary %q", out.SummaryNotes)
	}
	if out.SituationAnalysis != "Mày thấy da đỏ" {
		t.Fatalf("situation %q", out.SituationAnalysis)
	}
	if out.RoutineHints[0] != "Sáng: SPF50" || out.RoutineHints[1] != "Tối: ngày 7" {
		t.Fatalf("hints %#v", out.RoutineHints)
	}
	if out.Improvements[0].Tip != "Có 2–3 nốt" || out.Improvements[0].Why != "Má đỏ" {
		t.Fatalf("improvement %#v", out.Improvements[0])
	}
	var zones []map[string]string
	if err := json.Unmarshal(out.ZoneNotesRaw, &zones); err != nil {
		t.Fatal(err)
	}
	if zones[0]["zone"] != "left_cheek" || zones[0]["note"] != "Má trông giống mụn ẩn." {
		t.Fatalf("zone %#v", zones)
	}
	var scores map[string]string
	if err := json.Unmarshal(out.SkinScoreNotesRaw, &scores); err != nil {
		t.Fatal(err)
	}
	if scores["overall"] != "Da vẫn khỏe mạnh" || scores["clarity"] != "Có 2–3 nốt, SPF50, ngày 7" {
		t.Fatalf("scores %#v", scores)
	}
}

func TestCoachProseLeakDetectors(t *testing.T) {
	if !CoachProseHasScoreLeak("score 0.78 vì da khỏe") {
		t.Fatal("score sentence")
	}
	if !CoachProseHasScoreLeak("hydration: 0,78 vì da khô") {
		t.Fatal("gauge label decimal")
	}
	if CoachProseHasScoreLeak("Lấy 0,5 ml serum") || CoachProseHasScoreLeak("Retinol 0.025 hoặc 0.05") || CoachProseHasScoreLeak("BHA 0.5–2%") || CoachProseHasScoreLeak("Retinol: 0.025") {
		t.Fatal("dose, range, or ingredient strength counted as a score leak")
	}
	if CoachProseHasScoreLeak("da 0,78") {
		t.Fatal("bare decimal without a gauge label is not a leak")
	}
	if CoachProseHasScoreLeak("Có 2–3 nốt, SPF50, ngày 7, tăng 50%") {
		t.Fatal("ordinary numbers flagged")
	}
	if CoachProseHasScoreLeak("khoảng 0.5%") {
		t.Fatal("percentage flagged")
	}
	raw := `{"overall":0.78,"skin_score_notes":{"overall":"da khỏe"},"zone_notes":[{"zone":"left_cheek","note":"Mày thấy da đỏ"}]}`
	corpus := UserFacingCoachCorpus("Da ổn", raw)
	if CoachProseHasScoreLeak(corpus) {
		t.Fatal("numeric gauge or field key counted as a leak: " + corpus)
	}
	if !CoachProseHasTaoMay(corpus) {
		t.Fatal("mày in the note was missed")
	}
	if CoachProseHasTaoMay("kẻ mày") || CoachProseHasTaoMay("lông mày") || CoachProseHasTaoMay("Vùng mày") {
		t.Fatal("eyebrow mày must not count")
	}
	if !CoachProseHasTaoMay("và mày") || !CoachProseHasTaoMay("Tao thấy") {
		t.Fatal("pronoun tao/mày must still count")
	}
	if !strings.Contains(corpus, "da khỏe") {
		t.Fatalf("corpus %q", corpus)
	}
}

func TestUserFacingCoachCorpus_SkipsGaugeNumbers(t *testing.T) {
	blob := `{"overall":0.78,"hydration":0.5,"situation_analysis":"da ổn","care_suggestions":[{"step":"SPF50","why":"ngày 7"}]}`
	corpus := UserFacingCoachCorpus("", blob)
	if CoachProseHasScoreLeak(corpus) {
		t.Fatalf("gauges leaked into corpus %q", corpus)
	}
	var hint dto.CoachCareSuggestionItem
	_ = hint
}

func mustPublicJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
