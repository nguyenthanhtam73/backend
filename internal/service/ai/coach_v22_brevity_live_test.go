package ai

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// coachV22BrevityCaps mirrors the tightened v22 output limits so the live check can
// flag (not hard-fail) any field that drifts back to the verbose pre-v22 shape.
const (
	v22MaxSituationSentences = 3
	v22MaxImprovements       = 3
	v22MaxRoutineHints       = 4 // Normal mode ceiling (Beginner is 3)
	v22MinVisionDetails      = 3 // relaxed floor: specificity over length
)

var v22SentenceSplit = regexp.MustCompile(`[.!?…]+`)

func countSentences(s string) int {
	n := 0
	for _, part := range v22SentenceSplit.Split(s, -1) {
		if strings.TrimSpace(part) != "" {
			n++
		}
	}
	return n
}

// TestCoachV22_BrevityLive runs the coach against the four core Vietnamese skin
// concerns (da dầu mụn, da khô, da thâm nám, da nhạy cảm/barrier) using the LIVE
// model resolved from .env — which is now Claude 3.5 Haiku via the FastModel toggle.
// It reports per-scenario latency + brevity + vision-specificity so we can confirm
// the Haiku + brevity changes cut generation time without losing photo detail.
//
// Run: go test ./internal/service/ai/... -run TestCoachV22_BrevityLive -v -count=1 -timeout 30m
func TestCoachV22_BrevityLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live v22 brevity check in short mode")
	}
	cfg := loadCoachTestConfig(t)
	if strings.TrimSpace(cfg.Anthropic.APIKey) == "" && strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		t.Skip("set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	client := httpClientForCoachTests()

	// One representative scenario per requested skin type.
	all := VisionCoachScenarios()
	byID := map[string]VisionCoachScenario{}
	for _, sc := range all {
		byID[sc.ID] = sc
	}
	scenarios := []VisionCoachScenario{
		byID["oily_acne"],     // da dầu mụn
		byID["dry_skin"],      // da khô
		byID["melasma"],       // da thâm nám
		byID["combo_barrier"], // da nhạy cảm / barrier yếu
	}

	sep := strings.Repeat("=", 130)
	t.Log(sep)
	t.Logf("v22 brevity + Haiku live check | coach model=%s | prompt v%d | prompt chars(intermediate)=%d",
		cfg.AnthropicCoachModel(), CoachDailyPromptVersion, len(GetCoachPrompt("intermediate")))
	t.Log(sep)

	var totalElapsed time.Duration
	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			system := GetCoachPrompt(sc.Persona.SkillLevel)
			userMsg := sc.CoachUserMessage()

			start := time.Now()
			result, err := TextCoachCompletion(ctx, cfg, client, "v22-brevity-"+sc.ID, system, userMsg)
			elapsed := time.Since(start)
			totalElapsed += elapsed
			if err != nil {
				t.Fatalf("%s: coach failed: %v", sc.ID, err)
			}

			out, err := parseCoachStructuredOutput(result.Text, "v22-brevity")
			if err != nil {
				t.Fatalf("%s: parse failed (JSON schema broken!): %v", sc.ID, err)
			}

			sc.Persona.VisionJSON = sc.VisionJSON
			pers := ScoreCoachPersonalization(sc.Persona, out, true)
			sentCount := countSentences(out.SituationAnalysis)

			t.Logf("--- %s (%s) | provider=%s model=%s fallback=%v | %.1fs ---",
				sc.ID, sc.Label, result.Provider, result.Model, result.Fallback, elapsed.Seconds())
			t.Logf("  vision_details=%d (floor %d) | situation_sentences=%d (cap %d) | improvements=%d (cap %d) | routine_hints=%d (cap %d)",
				pers.VisionDetailCount, v22MinVisionDetails,
				sentCount, v22MaxSituationSentences,
				len(out.Improvements), v22MaxImprovements,
				len(out.RoutineHints), v22MaxRoutineHints)
			t.Logf("  opener=%v history=%v generic=%v report=%v",
				outputHasRequiredVisionOpener(out), pers.HasHistoryCallback,
				pers.HasGenericPhrases, pers.HasReportLikeTone)
			t.Logf("  situation_analysis: %q", out.SituationAnalysis)
			t.Logf("  improvements:")
			for i, imp := range out.Improvements {
				t.Logf("    %d. tip=%q why=%q", i+1, imp.Tip, imp.Why)
			}
			t.Logf("  routine_hints: %q", strings.Join(out.RoutineHints, " | "))
			t.Logf("  concern_alignment: %q", out.ConcernAlignment)
			t.Logf("  summary_notes: %q", out.SummaryNotes)

			// Quality floor: photo specificity must survive the trim. Hard-fail only
			// on the two things the task must not regress — JSON validity (already
			// checked above) and vision specificity.
			if pers.VisionDetailCount < v22MinVisionDetails {
				t.Errorf("%s: vision details %d below v22 floor %d — brevity hurt specificity",
					sc.ID, pers.VisionDetailCount, v22MinVisionDetails)
			}
			// Brevity caps are soft signals: log a warning instead of failing so
			// mild model overshoot doesn't block, but drift is still visible.
			if sentCount > v22MaxSituationSentences {
				t.Logf("  WARN %s: situation_analysis %d sentences (> cap %d)", sc.ID, sentCount, v22MaxSituationSentences)
			}
			if len(out.Improvements) > v22MaxImprovements {
				t.Logf("  WARN %s: improvements %d (> cap %d)", sc.ID, len(out.Improvements), v22MaxImprovements)
			}
			if len(out.RoutineHints) > v22MaxRoutineHints {
				t.Logf("  WARN %s: routine_hints %d (> cap %d)", sc.ID, len(out.RoutineHints), v22MaxRoutineHints)
			}
		})
	}

	t.Log(sep)
	t.Logf("v22 total coach wall time across %d scenarios: %.1fs (avg %.1fs/scenario)",
		len(scenarios), totalElapsed.Seconds(), totalElapsed.Seconds()/float64(len(scenarios)))
	t.Log(sep)
}
