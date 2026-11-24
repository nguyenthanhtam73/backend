package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

func TestWebCoachPrompt_NoProfanityAndMayCap(t *testing.T) {
	blobs := []string{
		GetCoachPrompt("beginner"),
		GetCoachPrompt("intermediate"),
		StarterRoutineSystemPrompt(),
		CoachOutputJSONSchemaBlock,
		coachTurnChecklistForVoice("VISION", true, ""),
	}
	banned := []string{"đm", "địt", "vcl", "vãi lồn", "đéo", "đụ", "thằng này", "bà này", "con này"}
	for _, blob := range blobs {
		low := strings.ToLower(blob)
		for _, word := range banned {
			if strings.Contains(low, word) {
				t.Fatalf("prompt contains banned %q", word)
			}
		}
		// "vl" and "cc" only count as their own words, not inside another word.
		if hasStandalone(low, "vl") || hasStandalone(low, "cc") {
			t.Fatal("prompt contains a banned short swear")
		}
	}
	web := GetCoachPrompt("intermediate")
	if !strings.Contains(web, "1–2 lần") {
		t.Fatal("web prompt missing the mày count rule")
	}
	if !strings.Contains(web, "Ê, hôm nay da mày nhìn êm hơn") {
		t.Fatal("web prompt missing an approved example")
	}
}

func TestSanitizeWebCoachOutput_DropsProfanityKeepsNumbers(t *testing.T) {
	out := &CoachStructuredOutput{
		SituationAnalysis: "Đm da mày hôm nay vl, score 0.78 vì da đỏ.",
		SummaryNotes:      "Tao thấy da mày ổn, có 2–3 nốt, SPF50, ngày 7.",
		RoutineHints:      []string{"Sáng: SPF50"},
		ZoneNotesRaw:      []byte(`[{"zone":"left_cheek","note":"ĐCM má mày đỏ.","severity":"mild"}]`),
		SkinScoreNotesRaw: []byte(`{"overall":"dcm da vẫn khỏe","clarity":"Có 2–3 nốt"}`),
		ProductSuggestions: []dto.ProductSuggestion{{
			Reason: "clgt hợp má mày", HowToUse: "Lấy 0,5 ml", ProductName: "Gel 0.5g",
		}},
	}
	if !SanitizeWebCoachOutput(out) {
		t.Fatal("expected a rewrite")
	}
	if strings.Contains(strings.ToLower(out.SituationAnalysis), "đm") || strings.Contains(strings.ToLower(out.SituationAnalysis), "vl") {
		t.Fatalf("profanity left: %q", out.SituationAnalysis)
	}
	if !strings.Contains(out.SituationAnalysis, "score 0.78") {
		t.Fatalf("numbers were stripped: %q", out.SituationAnalysis)
	}
	if !strings.Contains(out.SituationAnalysis, "mày") {
		t.Fatalf("pronoun was rewritten: %q", out.SituationAnalysis)
	}
	if out.SummaryNotes != "Tao thấy da mày ổn, có 2–3 nốt, SPF50, ngày 7." {
		t.Fatalf("clean summary changed: %q", out.SummaryNotes)
	}
	if string(out.ZoneNotesRaw) == "" || strings.Contains(strings.ToLower(string(out.ZoneNotesRaw)), "đcm") {
		t.Fatalf("zone note %s", out.ZoneNotesRaw)
	}
	if strings.Contains(strings.ToLower(string(out.SkinScoreNotesRaw)), "dcm") {
		t.Fatalf("gauge reason %s", out.SkinScoreNotesRaw)
	}
	if out.ProductSuggestions[0].Reason == "clgt hợp má mày" || out.ProductSuggestions[0].HowToUse != "Lấy 0,5 ml" {
		t.Fatalf("product %#v", out.ProductSuggestions[0])
	}
	if out.ProductSuggestions[0].ProductName != "Gel 0.5g" {
		t.Fatalf("product name changed: %q", out.ProductSuggestions[0].ProductName)
	}
	for _, in := range []string{"ĐCM", "dcm", "ĐMM", "cmn", "clgt"} {
		if got := SanitizeWebCoachText(in + " da mày"); strings.Contains(strings.ToLower(got), strings.ToLower(in)) {
			t.Fatalf("%s left in %q", in, got)
		}
	}
	if got := SanitizeWebCoachText("kẻ mày, vùng mày"); got != "kẻ mày, vùng mày" {
		t.Fatalf("eyebrow changed: %q", got)
	}
	if got := SanitizeWebCoachText("Lấy 0,5 ml serum"); got != "Lấy 0,5 ml serum" {
		t.Fatalf("dose changed: %q", got)
	}
	if got := SanitizeWebCoachText("Retinol 0.025 hoặc 0.05"); got != "Retinol 0.025 hoặc 0.05" {
		t.Fatalf("strength changed: %q", got)
	}
}

func TestReadPathStripsStoredProfanity(t *testing.T) {
	check := &domain.SkinCheck{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), UserID: uuid.MustParse("22222222-2222-2222-2222-222222222222")}
	analysis := &domain.SkinAnalysis{
		Status:       domain.AnalysisStatusCompleted,
		SummaryNotes: "Đm da mày hôm nay.",
		SkinScores: []byte(`{
			"situation_analysis":"vl da đỏ",
			"photo_limited_note":"đmm tối",
			"clarify_questions":["dcm đau không?"],
			"skin_score_notes":{"overall":"đcm da khỏe"},
			"zone_notes":[{"zone":"left_cheek","note":"clgt mày ơi","severity":"mild"}]
		}`),
	}
	res := dto.NewCreateSkinCheckResponse(check, analysis, nil)
	coach := res.Analysis.Coach
	if coach == nil {
		t.Fatal("missing coach")
	}
	if coach.SummaryNotes != "Da mày hôm nay." {
		t.Fatalf("summary %q", coach.SummaryNotes)
	}
	if strings.Contains(strings.ToLower(coach.SituationSummary), "vl") {
		t.Fatalf("situation %q", coach.SituationSummary)
	}
	if coach.PhotoLimitedNote != "Tối" && strings.Contains(strings.ToLower(coach.PhotoLimitedNote), "đmm") {
		t.Fatalf("photo note %q", coach.PhotoLimitedNote)
	}
	if len(coach.ClarifyQuestions) != 1 || strings.Contains(strings.ToLower(coach.ClarifyQuestions[0]), "dcm") {
		t.Fatalf("questions %#v", coach.ClarifyQuestions)
	}
	if coach.SkinScoreNotes == nil || strings.Contains(strings.ToLower(coach.SkinScoreNotes.Overall), "đcm") {
		t.Fatalf("gauge %#v", coach.SkinScoreNotes)
	}
	if len(coach.ZoneNotes) != 1 || coach.ZoneNotes[0].Zone != "left_cheek" || strings.Contains(strings.ToLower(coach.ZoneNotes[0].Note), "clgt") {
		t.Fatalf("zone %#v", coach.ZoneNotes)
	}
	if analysis.SummaryNotes != "Đm da mày hôm nay." {
		t.Fatal("stored summary was rewritten")
	}
}

func TestSanitizeWebStarterRoutine_AllFields(t *testing.T) {
	out := StarterRoutine{
		Encouragement: "Đm cố lên mày",
		SkinReadback:  "vl da đỏ",
		Morning:       []string{"Rửa mặt đcm"},
		ProductSuggestions: []dto.ProductSuggestion{{
			Reason: "cmn hợp", HowToUse: "Bôi 1,0 lớp",
		}},
	}
	if !SanitizeWebStarterRoutine(&out) {
		t.Fatal("expected a rewrite")
	}
	if strings.Contains(strings.ToLower(out.Encouragement), "đm") || !strings.Contains(out.Encouragement, "mày") {
		t.Fatalf("encouragement %q", out.Encouragement)
	}
	if strings.Contains(strings.ToLower(out.Morning[0]), "đcm") {
		t.Fatalf("morning %q", out.Morning[0])
	}
	if out.ProductSuggestions[0].HowToUse != "Bôi 1,0 lớp" || strings.Contains(strings.ToLower(out.ProductSuggestions[0].Reason), "cmn") {
		t.Fatalf("product %#v", out.ProductSuggestions[0])
	}
}

func TestAndroidCoachPrompt_GoldenUnchanged(t *testing.T) {
	// Hash of coach_prompt_polite.go output on main. This PR must not edit that file.
	want := map[string]string{
		"beginner":     androidPromptHash("beginner"),
		"intermediate": androidPromptHash("intermediate"),
	}
	_ = want
	for _, skill := range []string{"beginner", "intermediate", "advanced"} {
		p := GetCoachPromptForClient(skill, domain.RefreshClientAndroid)
		if !strings.Contains(p, "mình / bạn") {
			t.Fatalf("%s android prompt lost mình/bạn", skill)
		}
		if strings.Contains(p, "Ê, hôm nay da mày nhìn") || strings.Contains(p, "1–2 lần") {
			t.Fatalf("%s android prompt picked up the web rewrite", skill)
		}
		if skill != "advanced" && p != androidCoachPrompt(skill) {
			t.Fatal("android selector drifted")
		}
	}
	// Lock the bytes. Update only if coach_prompt_polite.go itself changes.
	if got := androidPromptHash("intermediate"); got != goldenAndroidIntermediateHash {
		t.Fatalf("android intermediate prompt changed: %s", got)
	}
	if got := androidPromptHash("beginner"); got != goldenAndroidBeginnerHash {
		t.Fatalf("android beginner prompt changed: %s", got)
	}
}

const (
	// SHA-256 of GetCoachPromptForClient on the untouched polite prompt.
	// Filled after the first hash run if these placeholders fail.
	goldenAndroidIntermediateHash = "ff06f689ec6f9262e87403639e8e342f81d72ca8bcee7a5bbf9e735b174c855c"
	goldenAndroidBeginnerHash     = "bb7960655c03219edd80b94fcdf1cbd00cdec73ee99c441e00564c953f64697a"
)

func androidPromptHash(skill string) string {
	sum := sha256.Sum256([]byte(GetCoachPromptForClient(skill, domain.RefreshClientAndroid)))
	return hex.EncodeToString(sum[:])
}

func hasStandalone(s, word string) bool {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			if b.String() == word {
				return true
			}
			b.Reset()
		}
	}
	return b.String() == word
}
