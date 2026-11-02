package mascot

import (
	"testing"
	"time"
)

func TestMissedStageTable(t *testing.T) {
	if len(missedStages) != 9 {
		t.Fatalf("stages: %d", len(missedStages))
	}
	seen := map[string]struct{}{}
	prev := 0
	for _, s := range missedStages {
		if s.MinDay <= prev {
			t.Fatalf("stages must increase: %+v", s)
		}
		prev = s.MinDay
		if s.Mood == "" || s.LineVI == "" || s.LineEN == "" {
			t.Fatalf("empty stage: %+v", s)
		}
		if _, dup := seen[s.Mood]; dup {
			t.Fatalf("duplicate mood %s", s.Mood)
		}
		seen[s.Mood] = struct{}{}
	}

	today := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		days   int
		mood   string
		lineVI string
		lineEN string
		messy  int
	}{
		{1, "normal", "Nay chăm da chưa nè?", "Skincare today yet?", 0},
		{2, "sleepy", "Hôm qua quên chăm da rồi nè", "You forgot your skincare yesterday", 0},
		{3, "sleepy", "Hôm qua quên chăm da rồi nè", "You forgot your skincare yesterday", 0},
		{4, "sad_pillow", "", "", 0},
		{5, "sad_pillow", "", "", 0},
		{6, "pouty_arms_crossed", "", "", 1},
		{8, "pouty_arms_crossed", "", "", 1},
		{9, "back_turned", "", "", 1},
		{11, "back_turned", "", "", 1},
		{12, "note_on_mirror", "Giận rồi đó!", "I'm sulking!", 2},
		{14, "note_on_mirror", "", "", 2},
		{15, "pot_on_head_corner", "", "", 3},
		{17, "pot_on_head_corner", "", "", 3},
		{18, "suitcase_leaving", "", "", 3},
		{20, "suitcase_leaving", "", "", 3},
		{21, "waiting_door_sign", "Tôi đã bị bỏ rơi ở đây", "I was abandoned here", 4},
		{40, "waiting_door_sign", "Tôi đã bị bỏ rơi ở đây", "I was abandoned here", 4},
	}
	for _, tc := range cases {
		got := Derive(Input{Today: today, Routines: missedOnly(today, tc.days)})
		if got.Mood != tc.mood || got.MissedDays != tc.days || got.MessyLevel != tc.messy {
			t.Fatalf("day %d: mood=%s missed=%d messy=%d", tc.days, got.Mood, got.MissedDays, got.MessyLevel)
		}
		if tc.lineVI != "" && (got.LineVI != tc.lineVI || got.LineEN != tc.lineEN) {
			t.Fatalf("day %d lines: %q / %q", tc.days, got.LineVI, got.LineEN)
		}
	}
}

func TestMessyLevelBands(t *testing.T) {
	if MessyLevel(0) != 0 || MessyLevel(5) != 0 {
		t.Fatal("below 6")
	}
	if MessyLevel(6) != 1 || MessyLevel(11) != 1 {
		t.Fatal("from 6")
	}
	if MessyLevel(12) != 2 || MessyLevel(14) != 2 {
		t.Fatal("from 12")
	}
	if MessyLevel(15) != 3 || MessyLevel(20) != 3 {
		t.Fatal("from 15")
	}
	if MessyLevel(21) != 4 || MessyLevel(80) != 4 {
		t.Fatal("from 21")
	}
}

func TestFinishedSessionResetsMissAndMessy(t *testing.T) {
	today := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	rows := missedOnly(today, 21)
	rows = append(rows, RoutineDay{
		Date:    today,
		Evening: []Step{{ID: "moisturizer", Completed: true}, {ID: "lip", Completed: true}},
	})
	latest := &CheckView{Conditions: []string{"breakout", "oily"}}
	got := Derive(Input{Today: today, Routines: rows, Latest: latest})
	if got.Mood != moodHug || got.MissedDays != 0 || got.MessyLevel != 0 {
		t.Fatalf("return: %+v", got)
	}
	if got.LineVI != "Ôm cái đã, rồi Bé Da vui." || got.LineEN != "A hug first, then Bé Da is happy." {
		t.Fatalf("hug lines: %q / %q", got.LineVI, got.LineEN)
	}
	if len(got.Marks) != 2 || got.Marks[0].ID != "acne" || got.Marks[1].ID != "oily" {
		t.Fatalf("marks stay: %+v", got.Marks)
	}
	if got.RoutineToday.PMDone != 2 || got.RoutineToday.PMTotal != 2 || got.LastStepDone != "lip" {
		t.Fatalf("routine: %+v step %s", got.RoutineToday, got.LastStepDone)
	}
}

func TestPartialTickDoesNotReset(t *testing.T) {
	today := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	got := Derive(Input{Today: today, Routines: []RoutineDay{
		{Date: today.AddDate(0, 0, -2), Morning: []Step{{ID: "wash", Completed: false}}},
		{Date: today, Morning: []Step{{ID: "wash", Completed: true}, {ID: "spf", Completed: false}}},
	}})
	if got.Mood != "sleepy" || got.MissedDays != 2 || got.MessyLevel != 0 {
		t.Fatalf("partial: mood=%s missed=%d messy=%d", got.Mood, got.MissedDays, got.MessyLevel)
	}
}

func TestMorningSessionCounts(t *testing.T) {
	today := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	got := Derive(Input{Today: today, Routines: []RoutineDay{
		{Date: today.AddDate(0, 0, -1), Morning: []Step{{ID: "wash", Completed: false}}},
		{Date: today, Morning: []Step{{ID: "wash", Completed: true}, {ID: "spf", Completed: true}}, Evening: []Step{{ID: "cream", Completed: false}}},
	}})
	if got.Mood != moodHug || got.MissedDays != 0 || got.MessyLevel != 0 {
		t.Fatalf("am session: %+v", got)
	}
	if got.RoutineToday.AMDone != 2 || got.RoutineToday.PMDone != 0 || got.LastStepDone != "spf" {
		t.Fatalf("counts: %+v %s", got.RoutineToday, got.LastStepDone)
	}
}

func TestNoHistoryIsHappy(t *testing.T) {
	today := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	got := Derive(Input{Today: today})
	if got.Mood != moodHappy || got.MissedDays != 0 || got.MessyLevel != 0 {
		t.Fatalf("%+v", got)
	}
	if got.LineVI != "Hôm nay da ổn." || got.LineEN != "Skin looks okay today." {
		t.Fatalf("lines %q %q", got.LineVI, got.LineEN)
	}
}

func TestSkinMarksLevelsAndLines(t *testing.T) {
	today := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	clarity := 0.2
	latest := &CheckView{
		At:         today.Add(8 * time.Hour),
		Conditions: []string{"oily", "breakout", "hyperpigmentation", "normal", "combo"},
		Symptoms:   []string{"post_acne_marks"},
		Zones: []ZoneCue{
			{Text: "má có nốt mụn", Severity: "moderate"},
			{Text: "da bóng vùng mũi", Severity: "mild"},
		},
		Gauges: Gauges{Clarity: &clarity},
	}
	prevClarity := 0.55
	prev := &CheckView{
		Conditions: []string{"hyperpigmentation"},
		Gauges:     Gauges{Clarity: &prevClarity},
	}
	got := Derive(Input{Today: today, Latest: latest, Previous: prev})
	if got.Mood != "dark_spot" {
		t.Fatalf("mood %s", got.Mood)
	}
	if got.LineVI != "Hôm nay thấy vết thâm nhẹ và nốt mới." || got.LineEN != "Today shows a faint mark and a few spots." {
		t.Fatalf("lines %q / %q", got.LineVI, got.LineEN)
	}
	want := map[string]string{"acne": levelMedium, "oily": levelLight, "dark_spot": levelHeavy}
	if len(got.Marks) != len(want) {
		t.Fatalf("marks %+v", got.Marks)
	}
	for _, m := range got.Marks {
		if want[m.ID] != m.Level {
			t.Fatalf("mark %+v", m)
		}
	}
	if len(got.MarksPrev) != 1 || got.MarksPrev[0].ID != "dark_spot" || got.MarksPrev[0].Level != levelMedium {
		t.Fatalf("prev %+v", got.MarksPrev)
	}
	if got.LastCheckIn == nil || !got.LastCheckIn.Equal(latest.At.UTC()) {
		t.Fatalf("check-in time")
	}
}

func TestSparkleWhenImproved(t *testing.T) {
	today := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	prevOverall, nextOverall := 0.40, 0.55
	got := Derive(Input{
		Today: today,
		Latest: &CheckView{
			Conditions: []string{"dry"},
			Gauges:     Gauges{Overall: &nextOverall, Hydration: &nextOverall},
		},
		Previous: &CheckView{
			Conditions: []string{"dry"},
			Gauges:     Gauges{Overall: &prevOverall, Hydration: &prevOverall},
		},
	})
	if got.Mood != moodSparkle {
		t.Fatalf("mood %s", got.Mood)
	}
	if got.LineVI != "Da khá hơn lần trước, vẫn thấy da hơi khô." {
		t.Fatalf("line %q", got.LineVI)
	}
}

func TestNewAcneIsNotSparkle(t *testing.T) {
	today := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	prevOverall, nextOverall := 0.40, 0.70
	got := Derive(Input{
		Today: today,
		Latest: &CheckView{
			Conditions: []string{"breakout"},
			Symptoms:   []string{"new_breakouts"},
			Gauges:     Gauges{Overall: &nextOverall},
		},
		Previous: &CheckView{
			Conditions: []string{"oily"},
			Gauges:     Gauges{Overall: &prevOverall},
		},
	})
	if got.Mood != "acne" {
		t.Fatalf("mood %s marks %+v", got.Mood, got.Marks)
	}
}

func TestMissedMoodKeepsMarks(t *testing.T) {
	today := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	got := Derive(Input{
		Today:    today,
		Routines: missedOnly(today, 6),
		Latest:   &CheckView{Conditions: []string{"oily", "breakout", "pih"}},
	})
	if got.Mood != "pouty_arms_crossed" || got.MessyLevel != 1 {
		t.Fatalf("mood %s messy %d", got.Mood, got.MessyLevel)
	}
	if len(got.Marks) != 3 {
		t.Fatalf("marks %+v", got.Marks)
	}
}

func TestGaugeFallbackAndSeverityMap(t *testing.T) {
	if mapSeverity("pronounced") != levelHeavy || mapSeverity("dense") != levelHeavy {
		t.Fatal("heavy")
	}
	if mapSeverity("moderate") != levelMedium || mapSeverity("nope") != "" {
		t.Fatal("map")
	}
	hydration := 0.5
	got := levelFor("dry", &CheckView{Gauges: Gauges{Hydration: &hydration}})
	if got != levelMedium {
		t.Fatalf("gauge %s", got)
	}
	got = levelFor("red", &CheckView{Zones: []ZoneCue{{Text: "má hơi đỏ", Severity: "mild"}}})
	if got != levelLight {
		t.Fatalf("zone %s", got)
	}
	got = levelFor("dry", &CheckView{Zones: []ZoneCue{{Text: "không thấy gì", Severity: "pronounced"}}})
	if got != levelLight {
		t.Fatalf("không must not read as dryness, got %s", got)
	}
}

func TestStreakSource(t *testing.T) {
	today := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	done := []Step{{ID: "wash", Completed: true}}
	rows := []RoutineDay{
		{Date: today, Morning: done},
		{Date: today.AddDate(0, 0, -1), Evening: done},
		{Date: today.AddDate(0, 0, -2), Morning: []Step{{ID: "wash", Completed: false}}},
	}
	got := Derive(Input{Today: today, Routines: rows})
	if got.StreakDays != 2 {
		t.Fatalf("routine streak %d", got.StreakDays)
	}
	n := 0
	got = Derive(Input{Today: today, Routines: rows, StreakFromRow: &n})
	if got.StreakDays != 0 {
		t.Fatalf("row streak wins, got %d", got.StreakDays)
	}
	n = 9
	got = Derive(Input{Today: today, Routines: rows, StreakFromRow: &n})
	if got.StreakDays != 9 {
		t.Fatalf("stored %d", got.StreakDays)
	}
}

func TestKeptUpSessionStaysHappy(t *testing.T) {
	today := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	done := []Step{{ID: "wash", Completed: true}}
	got := Derive(Input{Today: today, Routines: []RoutineDay{
		{Date: today, Morning: done},
		{Date: today.AddDate(0, 0, -1), Morning: done},
	}})
	if got.Mood != moodHappy || got.MissedDays != 0 || got.MessyLevel != 0 {
		t.Fatalf("%+v", got)
	}
}

func missedOnly(today time.Time, days int) []RoutineDay {
	if days <= 0 {
		return nil
	}
	return []RoutineDay{{
		Date:    today.AddDate(0, 0, -days),
		Morning: []Step{{ID: "wash", Completed: false}},
	}}
}
