package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
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

func TestSanitizeWebCoachOutput_DropsProfanityAndScore(t *testing.T) {
	out := &CoachStructuredOutput{
		SituationAnalysis: "Đm da mày hôm nay vl, score 0.78 vì da đỏ.",
		SummaryNotes:      "Tao thấy da mày ổn, có 2–3 nốt, SPF50, ngày 7.",
		RoutineHints:      []string{"Sáng: SPF50"},
	}
	if !SanitizeWebCoachOutput(out) {
		t.Fatal("expected a rewrite")
	}
	if strings.Contains(strings.ToLower(out.SituationAnalysis), "đm") || strings.Contains(strings.ToLower(out.SituationAnalysis), "vl") {
		t.Fatalf("profanity left: %q", out.SituationAnalysis)
	}
	if strings.Contains(strings.ToLower(out.SituationAnalysis), "score") || strings.Contains(out.SituationAnalysis, "0.78") {
		t.Fatalf("technical number left: %q", out.SituationAnalysis)
	}
	if !strings.Contains(out.SituationAnalysis, "mày") {
		t.Fatalf("pronoun was rewritten: %q", out.SituationAnalysis)
	}
	if out.SummaryNotes != "Tao thấy da mày ổn, có 2–3 nốt, SPF50, ngày 7." {
		t.Fatalf("clean summary changed: %q", out.SummaryNotes)
	}
	if out.RoutineHints[0] != "Sáng: SPF50" {
		t.Fatalf("hint %q", out.RoutineHints[0])
	}
	if got := SanitizeWebCoachText("kẻ mày, vùng mày"); got != "kẻ mày, vùng mày" {
		t.Fatalf("eyebrow changed: %q", got)
	}
	if got := SanitizeWebCoachText("khoảng 0,78% và 50%"); got != "khoảng 0,78% và 50%" {
		t.Fatalf("percent handling: %q", got)
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
