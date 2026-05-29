package ai

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
)

// TestCoachPersonaLive runs real LLM calls for each persona (with vs without memory)
// and logs personalization scores. Skipped when DADIARY_ANTHROPIC_API_KEY is unset.
//
// Run: go test ./internal/service/ai/... -run TestCoachPersonaLive -v -count=1
func TestCoachPersonaLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live coach persona test in short mode")
	}
	cfg := loadCoachTestConfig(t)
	if strings.TrimSpace(cfg.Anthropic.APIKey) == "" && strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		t.Skip("set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY to run live coach tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	for _, persona := range CoachPersonas() {
		t.Run(persona.ID+"/daily_feedback", func(t *testing.T) {
			runDailyFeedbackComparison(t, ctx, cfg, persona)
		})
		t.Run(persona.ID+"/routine_suggest", func(t *testing.T) {
			runRoutineSuggestComparison(t, ctx, cfg, persona)
		})
	}
}

func runDailyFeedbackComparison(t *testing.T, ctx context.Context, cfg *config.Config, p CoachPersona) {
	t.Helper()
	t.Logf("hybrid coach: anthropic=%v openai=%v claude_model=%q gpt_text=%q",
		cfg.HasAnthropicKey(), cfg.HasOpenAIKey(), cfg.AnthropicModel(), cfg.OpenAITextModel())

	outNoMem, err := GenerateDailyFeedback(ctx, cfg, p.TodayContextWithoutHistory(), p.SkillLevel)
	if err != nil {
		t.Fatalf("without memory: %v", err)
	}
	scoreNo := ScoreCoachPersonalization(p, outNoMem, false)
	t.Logf("[no memory] score=%.2f matched=%v missing=%v preview=%q",
		scoreNo.Score, scoreNo.MatchedWant, scoreNo.MissingWant, scoreNo.OutputPreview)
	t.Logf("[no memory] json=%s", CoachOutputJSON(outNoMem))

	outMem, err := GenerateDailyFeedback(ctx, cfg, p.FullContextWithMemory(), p.SkillLevel)
	if err != nil {
		t.Fatalf("with memory: %v", err)
	}
	LogCoachOutput("live-daily-feedback", p.ID, outMem)
	scoreMem := ScoreCoachPersonalization(p, outMem, true)
	t.Logf("[with memory] score=%.2f memory_only=%.2f history_cb=%v adherence=%v hints=%d matched=%v mem_only=%v avoid=%v",
		scoreMem.Score, scoreMem.MemoryOnlyScore, scoreMem.HasHistoryCallback, scoreMem.MentionsAdherence,
		scoreMem.RoutineHintCount, scoreMem.MatchedWant, scoreMem.MatchedMemoryOnly, scoreMem.HitAvoid)
	t.Logf("[with memory] preview=%q json=%s", scoreMem.OutputPreview, CoachOutputJSON(outMem))

	if !scoreNo.HasHistoryCallback && !scoreMem.HasHistoryCallback {
		t.Logf("WARN: neither run produced history callback")
	}
	if scoreMem.HasHistoryCallback && !scoreNo.HasHistoryCallback {
		t.Logf("OK: memory run added history callback")
	}
	if strings.Contains(p.Memory, "Routine adherence") && !scoreMem.MentionsAdherence {
		t.Errorf("with memory should mention adherence/routine effort, persona=%s", p.ID)
	}
	if p.ID == "beginner_oily" && scoreMem.RoutineHintCount > 3 {
		t.Logf("WARN: beginner_oily low adherence should cap hints ≤3, got %d", scoreMem.RoutineHintCount)
	}
	if scoreMem.MemoryOnlyScore < scoreNo.MemoryOnlyScore && len(p.WantWithMemoryOnly) > 0 {
		t.Logf("WARN: memory-only score (%.2f) not better than baseline (%.2f)", scoreMem.MemoryOnlyScore, scoreNo.MemoryOnlyScore)
	}
	if p.ID == "frequent_not_helpful" && len(scoreMem.HitAvoid) > 0 {
		t.Errorf("frequent_not_helpful persona should avoid BHA/actives after 👎, got hits: %v", scoreMem.HitAvoid)
	}
	if scoreMem.Score < 0.25 {
		t.Errorf("with-memory score too low (%.2f) — output may be too generic", scoreMem.Score)
	}
}

func runRoutineSuggestComparison(t *testing.T, ctx context.Context, cfg *config.Config, p CoachPersona) {
	t.Helper()

	inBase := SuggestRoutineInput{
		Profile:   p.Profile,
		LastCheck: p.TodayCheck,
		Locale:    "vi",
		SkillMode: p.SkillLevel,
	}
	rNo, err := GenerateSuggestedRoutine(ctx, cfg, inBase)
	if err != nil {
		t.Fatalf("routine without memory: %v", err)
	}
	scoreNo := ScoreRoutinePersonalization(p, rNo, false)
	t.Logf("[routine no memory] score=%.2f rationale=%q", scoreNo.Score, truncateRunes(rNo.Rationale, 120))

	inMem := inBase
	inMem.UserMemory = p.Memory
	rMem, err := GenerateSuggestedRoutine(ctx, cfg, inMem)
	if err != nil {
		t.Fatalf("routine with memory: %v", err)
	}
	LogSuggestedRoutineOutput(p.ID, rMem)
	scoreMem := ScoreRoutinePersonalization(p, rMem, true)
	t.Logf("[routine with memory] score=%.2f matched=%v avoid=%v rationale=%q",
		scoreMem.Score, scoreMem.MatchedWant, scoreMem.HitAvoid, truncateRunes(rMem.Rationale, 120))

	if p.ID == "frequent_not_helpful" && len(scoreMem.HitAvoid) > 0 {
		t.Errorf("routine should not mention avoided actives: %v", scoreMem.HitAvoid)
	}
}

func loadCoachTestConfig(t *testing.T) *config.Config {
	t.Helper()
	var fallback *config.Config
	for _, path := range coachTestEnvCandidates() {
		cfg, err := config.Load(path)
		if err != nil {
			continue
		}
		if coachTestConfigHasLLMKey(cfg) {
			return cfg
		}
		if fallback == nil {
			fallback = cfg
		}
	}
	if fallback != nil {
		return fallback
	}
	t.Fatalf("could not load config from .env")
	return nil
}

// coachTestEnvCandidates returns .env paths relative to the test package dir
// (go test sets cwd to the package under test, not module root).
func coachTestEnvCandidates() []string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return []string{"../.env", "../../.env", ".env"}
	}
	base := filepath.Dir(file) // .../internal/service/ai
	return []string{
		filepath.Join(base, "..", "..", "..", "..", ".env"), // repo root (DaDiary-transfer)
		filepath.Join(base, "..", "..", "..", ".env"),       // backend/
		filepath.Join(base, "..", "..", "..", "..", "backend", ".env"),
		"../.env",
		".env",
	}
}

func coachTestConfigHasLLMKey(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return strings.TrimSpace(cfg.Anthropic.APIKey) != "" ||
		strings.TrimSpace(cfg.OpenAI.APIKey) != ""
}
