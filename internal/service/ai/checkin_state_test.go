package ai

import (
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestFormatCheckInStateBlock_FirstVisitAndGoodScore(t *testing.T) {
	score := 0.8
	facts := CheckInCoachFacts{
		CountKnown:         true,
		TotalIncludesToday: 1,
		TodayRating:        "good",
		TodayScoreText:     "4",
		History: []CheckInHistoryFact{{
			Date:  "2026-10-01",
			Score: &score,
			Tags:  "redness",
			Note:  "má đỏ",
		}},
	}
	// History means this is not the first visit, even if the count says 1.
	if facts.FirstEver() {
		t.Fatal("history should clear first-ever")
	}
	block := FormatCheckInStateBlock(facts)
	for _, needle := range []string{
		"check_in_count_including_today: 1",
		"first_check_in: no",
		"today_skin: good",
		"today_score: 4",
		"score: 0.8",
		"tags: redness",
		"muốn xem vùng đó dịu hơn không",
	} {
		if !strings.Contains(block, needle) {
			t.Fatalf("block missing %q:\n%s", needle, block)
		}
	}

	first := CheckInCoachFacts{CountKnown: true, TotalIncludesToday: 1, TodayRating: "fine", TodayScoreText: "ổn"}
	if !first.FirstEver() {
		t.Fatal("count 1 and no history is the first check-in")
	}
	firstBlock := FormatCheckInStateBlock(first)
	if !strings.Contains(firstBlock, "first_check_in: yes") || !strings.Contains(firstBlock, "history: none") {
		t.Fatalf("first block:\n%s", firstBlock)
	}
	if !strings.Contains(firstBlock, "chăm sóc da đều đặn") {
		t.Fatal("first-check guardrail missing")
	}
}

func TestDeriveCheckInCoachFacts_RatingSources(t *testing.T) {
	fromScore := DeriveCheckInCoachFacts(&domain.SkinCheck{
		ClimateContext: []byte(`{"ui_locale":"vi","score":4}`),
		Conditions:     []byte(`["redness"]`),
	})
	if fromScore.TodayRating != "good" || fromScore.TodayScoreText != "4" {
		t.Fatalf("numeric chip: %+v", fromScore)
	}

	fromWord := DeriveCheckInCoachFacts(&domain.SkinCheck{
		ClimateContext: []byte(`{"skin_rating":"fine"}`),
	})
	if !fromWord.SkinFineOrGood() || fromWord.TodayRating != "fine" {
		t.Fatalf("word rating: %+v", fromWord)
	}

	fromNote := DeriveCheckInCoachFacts(&domain.SkinCheck{UserNote: "Hôm nay da ổn, ngủ đủ."})
	if fromNote.TodayRating != "fine" {
		t.Fatalf("note: %+v", fromNote)
	}
	uneasyNote := DeriveCheckInCoachFacts(&domain.SkinCheck{UserNote: "Da không ổn, nổi mụn."})
	if uneasyNote.SkinFineOrGood() {
		t.Fatalf("negative note should not be fine: %+v", uneasyNote)
	}

	normal := DeriveCheckInCoachFacts(&domain.SkinCheck{Conditions: []byte(`["normal"]`)})
	if normal.TodayRating != "fine" {
		t.Fatalf("normal tag: %+v", normal)
	}
	red := DeriveCheckInCoachFacts(&domain.SkinCheck{Conditions: []byte(`["normal","redness"]`)})
	if red.TodayRating != "" {
		t.Fatalf("mixed tags are not a fine rating: %+v", red)
	}
}

func TestApplyCheckInAdviceGuard_FirstCheckInAndFineSkin(t *testing.T) {
	first := CheckInCoachFacts{CountKnown: true, TotalIncludesToday: 1}
	out := &CoachStructuredOutput{
		Strengths:         []string{"Bạn chăm sóc da đều đặn quá.", "Đã gửi ảnh rõ."},
		SummaryNotes:      "Kiên trì vậy mai sẽ khác.",
		SituationAnalysis: "Hôm nay má hơi đỏ. Bạn duy trì đều quá.",
	}
	if !ApplyCheckInAdviceGuard(out, first, domain.RefreshClientAndroid) {
		t.Fatal("expected a rewrite")
	}
	joined := strings.Join(out.Strengths, " ") + " " + out.SummaryNotes + " " + out.SituationAnalysis
	for _, banned := range []string{"đều đặn", "Kiên trì", "duy trì đều"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("still praises consistency %q: %s", banned, joined)
		}
	}
	if !strings.Contains(out.SummaryNotes, "lần check-in đầu tiên") {
		t.Fatalf("summary: %q", out.SummaryNotes)
	}
	if !strings.Contains(strings.Join(out.Strengths, " "), "Đã gửi ảnh rõ") {
		t.Fatalf("kept strength dropped: %#v", out.Strengths)
	}

	returning := CheckInCoachFacts{CountKnown: true, TotalIncludesToday: 4}
	keep := &CoachStructuredOutput{SummaryNotes: "Bạn chăm sóc da đều đặn."}
	if ApplyCheckInAdviceGuard(keep, returning, "") {
		t.Fatal("returning user may be praised for consistency")
	}

	fine := CheckInCoachFacts{CountKnown: true, TotalIncludesToday: 3, TodayRating: "good", TodayScoreText: "good"}
	calm := &CoachStructuredOutput{
		SummaryNotes: "Da hôm nay ổn. Mai chụp cùng góc nhé — mình muốn xem vùng đó dịu hơn không.",
		CareSuggestions: []CoachCareSuggestion{{
			Step: "Rửa mặt dịu",
			Why:  "Giữ bước dịu vì da đang ổn.",
		}},
	}
	if !ApplyCheckInAdviceGuard(calm, fine, domain.RefreshClientAndroid) {
		t.Fatal("expected calm-down question to be rewritten")
	}
	if strings.Contains(calm.SummaryNotes, "dịu hơn") {
		t.Fatalf("summary still asks to calm down: %q", calm.SummaryNotes)
	}
	if !strings.Contains(calm.SummaryNotes, "Da hôm nay ổn.") || !strings.Contains(calm.SummaryNotes, "giữ được mức này") {
		t.Fatalf("summary: %q", calm.SummaryNotes)
	}
	if calm.CareSuggestions[0].Step != "Rửa mặt dịu" {
		t.Fatalf("gentle cleanse step was rewritten: %+v", calm.CareSuggestions[0])
	}

	sore := CheckInCoachFacts{CountKnown: true, TotalIncludesToday: 3, TodayRating: "uneasy"}
	ask := &CoachStructuredOutput{SummaryNotes: "Mai chụp cùng góc nhé — mình muốn xem vùng đó dịu hơn không."}
	if ApplyCheckInAdviceGuard(ask, sore, domain.RefreshClientAndroid) {
		t.Fatal("uneasy skin may ask whether the area calms down")
	}
}

func TestCoachPrompt_v30_CheckInStateGuard(t *testing.T) {
	if CoachDailyPromptVersion != 30 {
		t.Fatalf("prompt version = %d, want 30", CoachDailyPromptVersion)
	}
	for _, p := range []string{
		GetCoachPrompt("intermediate"),
		GetCoachPromptForClient("beginner", domain.RefreshClientAndroid),
	} {
		if !strings.Contains(p, "Khớp trạng thái thật") || !strings.Contains(p, "chăm sóc da đều đặn") || !strings.Contains(p, "muốn xem vùng đó dịu hơn không") {
			t.Fatal("system prompt missing check-in state guard")
		}
	}
	web := buildSkinCheckCoachUserMessage(&domain.SkinCheck{ClientKind: domain.RefreshClientWeb}, nil, "", `{"visible_observations":["má"]}`, "ok", "", CheckInCoachFacts{
		CountKnown: true, TotalIncludesToday: 1, TodayRating: "good", TodayScoreText: "good",
	})
	if !strings.Contains(web, "CHECK_IN_STATE") || !strings.Contains(web, "first_check_in: yes") || !strings.Contains(web, "today_score: good") {
		t.Fatal("coach user message missing check-in state")
	}
	if !strings.Contains(web, "FIRST CHECK-IN") || !strings.Contains(web, "SKIN FINE/GOOD") {
		t.Fatal("checklist missing state guardrails")
	}
}

func TestApplyCheckInAdviceGuard_MeaninglessTipReplacedOrDropped(t *testing.T) {
	first := CheckInCoachFacts{CountKnown: true, TotalIncludesToday: 1}
	fine := CheckInCoachFacts{CountKnown: true, TotalIncludesToday: 3, TodayRating: "good", TodayScoreText: "good"}

	for _, tc := range []struct {
		name  string
		kind  string
		facts CheckInCoachFacts
		want  string
	}{
		{name: "web", kind: domain.RefreshClientWeb, facts: first, want: "Hôm nay giữ bước dịu: rửa mặt nhẹ rồi dưỡng ẩm."},
		{name: "android", kind: domain.RefreshClientAndroid, facts: first, want: "Hôm nay bạn giữ bước dịu: rửa mặt nhẹ rồi dưỡng ẩm."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &CoachStructuredOutput{
				Improvements: []struct {
					Tip string `json:"tip"`
					Why string `json:"why"`
				}{
					{Tip: "Bạn chăm sóc da đều đặn!!!", Why: "Kiên trì nhé."},
					{Tip: "Rửa mặt dịu. Nhớ chăm sóc da đều đặn.", Why: "Giữ bước dịu vì da đang ổn."},
				},
				RoutineHints: []string{"Kiên trì.", "Bạn quá. Duy trì đều quá.", "Rửa mặt dịu"},
			}
			if !ApplyCheckInAdviceGuard(out, tc.facts, tc.kind) {
				t.Fatal("expected a rewrite")
			}
			if len(out.Improvements) != 2 {
				t.Fatalf("improvements: %+v", out.Improvements)
			}
			if out.Improvements[0].Tip != tc.want || out.Improvements[0].Why != tc.want {
				t.Fatalf("gutted tip = %#v, want neutral %q", out.Improvements[0], tc.want)
			}
			if out.Improvements[1].Tip != "Rửa mặt dịu." {
				t.Fatalf("real step lost: %#v", out.Improvements[1])
			}
			if out.Improvements[1].Why != "Giữ bước dịu vì da đang ổn." {
				t.Fatalf("real why rewritten: %#v", out.Improvements[1])
			}
			if len(out.RoutineHints) != 1 || out.RoutineHints[0] != "Rửa mặt dịu" {
				t.Fatalf("routine hints = %#v", out.RoutineHints)
			}
			assertNoEmptyOrLeftoverTips(t, out)
		})
	}

	t.Run("fine skin drops calm-down leftover", func(t *testing.T) {
		for _, tc := range []struct {
			kind string
			want string
		}{
			{kind: "", want: "Hôm nay giữ bước dịu: rửa mặt nhẹ rồi dưỡng ẩm."},
			{kind: domain.RefreshClientAndroid, want: "Hôm nay bạn giữ bước dịu: rửa mặt nhẹ rồi dưỡng ẩm."},
		} {
			out := &CoachStructuredOutput{
				CareSuggestions: []CoachCareSuggestion{
					{Step: "Muốn xem vùng đó dịu hơn không?", Why: "!!!", SafetyNote: "Nhớ nhé."},
					{Step: "Rửa mặt dịu", Why: "Giữ bước dịu vì da đang ổn."},
				},
				AvoidOrPatch: []string{".... Muốn xem da dịu hơn không?", "Bạn quá. Muốn xem da dịu hơn không?"},
			}
			if !ApplyCheckInAdviceGuard(out, fine, tc.kind) {
				t.Fatal("expected a rewrite")
			}
			if len(out.CareSuggestions) != 2 {
				t.Fatalf("care suggestions: %+v", out.CareSuggestions)
			}
			if out.CareSuggestions[0].Step != tc.want || out.CareSuggestions[0].Why != tc.want {
				t.Fatalf("calm-down step = %#v, want %q", out.CareSuggestions[0], tc.want)
			}
			if out.CareSuggestions[0].SafetyNote != "" {
				t.Fatalf("leftover safety note %q", out.CareSuggestions[0].SafetyNote)
			}
			if out.CareSuggestions[1].Step != "Rửa mặt dịu" {
				t.Fatalf("gentle cleanse changed: %#v", out.CareSuggestions[1])
			}
			if len(out.AvoidOrPatch) != 0 {
				t.Fatalf("avoid list = %#v", out.AvoidOrPatch)
			}
			assertNoEmptyOrLeftoverTips(t, out)
		}
	})
}

func assertNoEmptyOrLeftoverTips(t *testing.T, out *CoachStructuredOutput) {
	t.Helper()
	check := func(label, s string) {
		t.Helper()
		if strings.TrimSpace(s) == "" || isMeaninglessAdvice(s) {
			t.Fatalf("%s is empty or leftover: %q", label, s)
		}
	}
	for i, s := range out.RoutineHints {
		check("routine hint "+string(rune('a'+i)), s)
	}
	for i, s := range out.AvoidOrPatch {
		check("avoid "+string(rune('a'+i)), s)
	}
	for i, s := range out.SafetyReminders {
		check("safety "+string(rune('a'+i)), s)
	}
	for i, imp := range out.Improvements {
		check("improvement tip", imp.Tip)
		check("improvement why", imp.Why)
		if strings.TrimSpace(imp.Tip) == "" || strings.TrimSpace(imp.Why) == "" {
			t.Fatalf("improvement %d empty: %#v", i, imp)
		}
	}
	for i, sug := range out.CareSuggestions {
		check("care step", sug.Step)
		check("care why", sug.Why)
		if strings.TrimSpace(sug.SafetyNote) != "" {
			check("care safety", sug.SafetyNote)
		}
		if strings.TrimSpace(sug.Step) == "" {
			t.Fatalf("care step %d empty: %#v", i, sug)
		}
	}
}

func TestHistoryFactsFromChecks_IncludesScore(t *testing.T) {
	when := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	rows := HistoryFactsFromChecks([]domain.SkinCheck{{
		ID:         uuid.New(),
		CheckDate:  when,
		Conditions: []byte(`["oily"]`),
		UserNote:   "trán bóng",
		Analysis: &domain.SkinAnalysis{
			Status:     domain.AnalysisStatusCompleted,
			SkinScores: []byte(`{"overall":0.62}`),
		},
	}})
	if len(rows) != 1 || rows[0].Date != "2026-10-20" || rows[0].Score == nil || *rows[0].Score != 0.62 {
		t.Fatalf("history: %+v", rows)
	}
	if rows[0].Tags != "oily" || rows[0].Note != "trán bóng" {
		t.Fatalf("history text: %+v", rows[0])
	}
}
