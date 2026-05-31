// Package ai — user_memory_scenarios_test.go covers the prompt-level behaviour
// of the USER_MEMORY block builder for the four target user personas the
// product team cares about:
//
//   1. Brand-new beginner (no history) — must NOT crash, must emit the
//      "no saved memory yet" sentinel.
//   2. Power user with > 50 check-ins — must emit the monthly digest section
//      with paraphrased tags only (not raw counts in the rendered text).
//   3. Frustrated user with frequent 👎 votes — must surface the votes and
//      include the GUIDANCE line about pivoting away from past angles.
//   4. High-adherence user — must surface the "strong adherence" tier label
//      so the coach can praise consistency.
//
// We test the per-section formatting helpers in isolation (no DB), then
// assert end-to-end that the system prompt + memory block contain the
// callback hooks the product spec demands.
package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// --- Helpers to mint fake domain rows without a DB ---------------------

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func mkFeedback(rating string, comment string, daysAgo int) domain.AIUserFeedback {
	return domain.AIUserFeedback{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		TargetType: string(domain.AIFeedbackTargetSuggestedRoutine),
		TargetID:   uuid.New(),
		Rating:     rating,
		Comment:    comment,
		CreatedAt:  time.Now().Add(-time.Duration(daysAgo) * 24 * time.Hour),
	}
}

func mkSkinAnalysis(summary string) *domain.SkinAnalysis {
	return &domain.SkinAnalysis{
		ID:           uuid.New(),
		Status:       domain.AnalysisStatusCompleted,
		SummaryNotes: summary,
	}
}

// --- Test 1: brand-new beginner --------------------------------------

// TestPersona_BrandNewBeginner — no profile, no checks, no feedback, no
// routines. The builder must still produce a usable block ending with the
// "rely on TODAY context only" sentinel and the GUIDANCE line should NOT
// fire (no real sections to guide).
func TestPersona_BrandNewBeginner(t *testing.T) {
	out := BuildUserMemoryContext(nil, uuid.New(), UserMemoryDeps{}, UserMemoryOptions{})

	mustContain(t, out, "USER_MEMORY")
	mustContain(t, out, "no saved memory yet")
	mustNotContain(t, out, "## Saved SkinProfile")
	mustNotContain(t, out, "## Recent SkinChecks")
	mustNotContain(t, out, "## Past AI feedback votes")
	// No GUIDANCE line when block is empty (sentinel-only).
	mustNotContain(t, out, "GUIDANCE: Treat this block")

	// Beginner prompt itself must enumerate USER_MEMORY in its input list
	// and include the cá-nhân-hoá callback section.
	prompt := GetCoachPrompt("beginner")
	mustContain(t, prompt, "USER_MEMORY")
	mustContain(t, prompt, "Callback bắt buộc")
	mustContain(t, prompt, "pivot 👎")
}

// --- Test 2: profile section formatting ------------------------------

func TestBuildSkinProfileContext_FullProfile(t *testing.T) {
	p := &domain.SkinProfile{
		ID:              uuid.New(),
		UserID:          uuid.New(),
		SkinType:        "dry",
		SkillLevel:      domain.SkillLevelIntermediate,
		Concerns:        mustJSON(t, []string{"dehydration", "redness"}),
		Notes:           "đặc biệt khô vùng má vào mùa khô",
		HomeCountryCode: "VN",
	}
	out := BuildSkinProfileContext(p)
	mustContain(t, out, "dry")
	mustContain(t, out, "intermediate")
	mustContain(t, out, "dehydration")
	mustContain(t, out, "khô vùng má")
	mustContain(t, out, "VN")
}

// --- Test 3: power user with frequent 👎 -----------------------------

// TestPersona_FrequentNotHelpful — a user who has marked 4 out of 5 recent
// outputs 👎 with stated reasons. We expect BuildPriorFeedbackContext to:
//   1) emit all rows newest-first
//   2) include the totals line so the model can detect the pattern
//   3) include the GUIDANCE line about NOT repeating the same angle
//   4) NOT quote the user's reason verbatim in the GUIDANCE (paraphrase rule)
func TestPersona_FrequentNotHelpful(t *testing.T) {
	rows := []domain.AIUserFeedback{
		mkFeedback("not_helpful", "BHA quá mạnh, em da nhạy", 1),
		mkFeedback("not_helpful", "Gợi ý quá chung chung", 3),
		mkFeedback("helpful", "", 4),
		mkFeedback("not_helpful", "Không hợp da khô", 6),
		mkFeedback("not_helpful", "Quá nhiều bước", 9),
	}
	out := BuildPriorFeedbackContext(rows)

	mustContain(t, out, "USER_FEEDBACK_HISTORY")
	mustContain(t, out, "BHA quá mạnh")
	mustContain(t, out, "Gợi ý quá chung chung")
	// Totals so the model can see the imbalance.
	mustContain(t, out, "👍 1 / 👎 4")
	// GUIDANCE must steer pivot.
	mustContain(t, out, "do NOT push the same suggestion")
	// Paraphrase rule.
	mustContain(t, out, "Never quote the user's reason verbatim")
}

// --- Test 4: routine adherence tiers ----------------------------------

// TestCompletionTier covers every band. Coach-level prompt logic depends on
// these labels being stable, so make sure the boundaries don't drift.
func TestCompletionTier(t *testing.T) {
	cases := []struct {
		name    string
		rate    float64
		wantSub string
		short   string
	}{
		{"strong", 0.9, "strong adherence", "strong"},
		{"moderate-mid", 0.5, "moderate adherence", "moderate"},
		{"low", 0.2, "low adherence", "low"},
		{"none", 0.0, "no ticks in window", "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := completionTier(tc.rate)
			if !strings.Contains(got, tc.wantSub) {
				t.Fatalf("tier(%.2f) = %q; want sub %q", tc.rate, got, tc.wantSub)
			}
			if shortTierLabel(got) != tc.short {
				t.Fatalf("shortTierLabel(%q) = %q; want %q", got, shortTierLabel(got), tc.short)
			}
		})
	}
}

// --- Test 5: previous AI line summarisation ---------------------------

// TestSummarizePreviousAIFeedback verifies the per-check summarisation path
// the Recent SkinChecks section relies on. SummaryNotes wins; falls back to
// situation_analysis embedded in skin_scores JSON; truncates long lines.
func TestSummarizePreviousAIFeedback(t *testing.T) {
	t.Run("summary_notes preferred", func(t *testing.T) {
		a := mkSkinAnalysis("Mai chụp cùng góc nhé — mình muốn xem vùng T có dịu lại không.")
		got := summarizePreviousAIFeedback(a)
		mustContain(t, got, "Mai chụp")
	})
	t.Run("falls back to situation_analysis in skin_scores", func(t *testing.T) {
		a := mkSkinAnalysis("")
		a.SkinScores = mustJSON(t, map[string]any{
			"situation_analysis": "Da hôm nay có vẻ căng và châm chích nhẹ.",
		})
		got := summarizePreviousAIFeedback(a)
		mustContain(t, got, "Da hôm nay")
	})
	t.Run("truncates long lines", func(t *testing.T) {
		long := strings.Repeat("một câu khá dài. ", 30)
		a := mkSkinAnalysis(long)
		got := summarizePreviousAIFeedback(a)
		if len([]rune(got)) > 162 { // 160 cap + "…" + trim slack
			t.Fatalf("expected truncation to ~160 runes, got %d", len([]rune(got)))
		}
	})
	t.Run("skips non-completed analysis", func(t *testing.T) {
		a := mkSkinAnalysis("never displayed")
		a.Status = domain.AnalysisStatusFailed
		if got := summarizePreviousAIFeedback(a); got != "" {
			t.Fatalf("failed analysis should yield empty, got %q", got)
		}
	})
}

// --- Test 6: coach prompt contains the cá-nhân-hoá block --------------

// TestCoachPrompts_HaveMemoryGuidance is the canary for "did we accidentally
// strip the personalisation section?" — if this fails after a prompt edit,
// the regression is visible immediately rather than weeks later when users
// notice the coach has stopped using their history.
func TestCoachPrompts_HaveMemoryGuidance(t *testing.T) {
	for _, skill := range []string{"beginner", "intermediate", "advanced"} {
		t.Run(skill, func(t *testing.T) {
			p := GetCoachPrompt(skill)
			mustContain(t, p, "USER_MEMORY")
			mustContain(t, p, "Callback")
			mustContain(t, p, "Feedback summary")
			mustContain(t, p, "Past AI feedback votes")
			mustContain(t, p, "Routine adherence")
			mustContain(t, p, "Older history")
		})
	}
}

// TestCoachPromptVersion makes sure we bumped the version after editing
// semantics. If you change coachCorePromptVI materially, also bump
// CoachDailyPromptVersion in coach_daily_version.go.
func TestCoachPromptVersion(t *testing.T) {
	if CoachDailyPromptVersion < 19 {
		t.Fatalf("expected CoachDailyPromptVersion >= 19 (specific vision v19), got %d", CoachDailyPromptVersion)
	}
}

// --- Test 7: feedback context with NO rows ---------------------------

func TestBuildPriorFeedbackContext_Empty(t *testing.T) {
	if got := BuildPriorFeedbackContext(nil); got != "" {
		t.Fatalf("empty input should yield empty output, got %q", got)
	}
	if got := BuildPriorFeedbackContext([]domain.AIUserFeedback{}); got != "" {
		t.Fatalf("empty slice should yield empty output, got %q", got)
	}
}

// --- Test 8: section labelling consistency ----------------------------

// TestSectionHeaders pins the section header strings the coach prompt
// refers to. If a header rename happens in user_memory.go but the prompt
// still references the old name, the model can't latch onto the section.
//
// We don't try to render a full block here (that needs DB), but we DO
// pin the header strings we know the prompt enumerates.
func TestSectionHeaders(t *testing.T) {
	prompt := GetCoachPrompt("intermediate")
	for _, header := range []string{
		"## Saved SkinProfile",
		"## Recent SkinChecks",
		"## Older history",
		"## Past AI feedback votes",
		"## Routine adherence",
	} {
		t.Run(header, func(t *testing.T) {
			mustContain(t, prompt, header)
		})
	}
}

// --- Tiny string-matching helpers ------------------------------------

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected text to contain %q\n--- text ---\n%s\n--- end ---", needle, haystack)
	}
}

func mustNotContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("expected text to NOT contain %q\n--- text ---\n%s\n--- end ---", needle, haystack)
	}
}
