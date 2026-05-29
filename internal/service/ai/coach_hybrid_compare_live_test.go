package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
)

// TestCoachHybridPersonaCompare runs the same 4 personas through Claude Sonnet vs GPT-4o
// (daily feedback + routine suggest, with memory) and prints a comparison table.
//
// Run: go test ./internal/service/ai/... -run TestCoachHybridPersonaCompare -v -count=1 -timeout 25m
func TestCoachHybridPersonaCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live A/B compare test in short mode")
	}
	cfg := loadCoachTestConfig(t)
	if !cfg.HasAnthropicKey() || !cfg.HasOpenAIKey() {
		t.Skip("set both DADIARY_ANTHROPIC_API_KEY and DADIARY_OPENAI_API_KEY for A/B compare")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 22*time.Minute)
	defer cancel()

	var rows []compareRow
	claudeWins, gptWins, ties := 0, 0, 0
	var claudeLatTotal, gptLatTotal time.Duration
	var claudeTokTotal, gptTokTotal int

	for _, persona := range CoachPersonas() {
		t.Run(persona.ID, func(t *testing.T) {
			// --- Daily feedback (with memory) ---
			system, user := buildDailyFeedbackPrompt(persona.FullContextWithMemory(), persona.SkillLevel)

			claudeRes := callCompareLLM(ctx, cloneConfigForProvider(cfg, TextCoachProviderClaude), TextCoachProviderClaude, system, user)
			gptRes := callCompareLLM(ctx, cloneConfigForProvider(cfg, TextCoachProviderOpenAI), TextCoachProviderOpenAI, system, user)
			if claudeRes.Err != nil {
				t.Fatalf("claude daily feedback: %v", claudeRes.Err)
			}
			if gptRes.Err != nil {
				t.Fatalf("gpt daily feedback: %v", gptRes.Err)
			}

			claudeOut, err := parseCoachStructuredOutput(claudeRes.Text, "compare-claude")
			if err != nil {
				t.Fatalf("claude parse: %v", err)
			}
			gptOut, err := parseCoachStructuredOutput(gptRes.Text, "compare-gpt")
			if err != nil {
				t.Fatalf("gpt parse: %v", err)
			}

			claudeQ := BuildCoachQualityReport(persona, claudeOut, true)
			gptQ := BuildCoachQualityReport(persona, gptOut, true)

			// --- Routine suggest (with memory) ---
			in := SuggestRoutineInput{
				Profile:    persona.Profile,
				LastCheck:  persona.TodayCheck,
				Locale:     "vi",
				SkillMode:  persona.SkillLevel,
				UserMemory: persona.Memory,
			}
			rSystem := suggestRoutineSystemPrompt()
			rUser := buildSuggestRoutineUserMessage(in, "vi", normalizeRoutineSkillLevel(persona.SkillLevel, persona.Profile))

			claudeRLLM := callCompareLLM(ctx, cloneConfigForProvider(cfg, TextCoachProviderClaude), TextCoachProviderClaude, rSystem, rUser)
			gptRLLM := callCompareLLM(ctx, cloneConfigForProvider(cfg, TextCoachProviderOpenAI), TextCoachProviderOpenAI, rSystem, rUser)

			var claudeR, gptR RoutineQualityReport
			var claudeRErr, gptRErr error
			if claudeRLLM.Err == nil {
				if r, pErr := parseSuggestedRoutine(claudeRLLM.Text); pErr == nil {
					claudeR = BuildRoutineQualityReport(persona, r, true)
				} else {
					claudeRErr = pErr
				}
			} else {
				claudeRErr = claudeRLLM.Err
			}
			if gptRLLM.Err == nil {
				if r, pErr := parseSuggestedRoutine(gptRLLM.Text); pErr == nil {
					gptR = BuildRoutineQualityReport(persona, r, true)
				} else {
					gptRErr = pErr
				}
			} else {
				gptRErr = gptRLLM.Err
			}

			r := compareRow{
				personaID:  persona.ID,
				pipeline:   "daily_feedback",
				claudeQ:    claudeQ,
				gptQ:       gptQ,
				claudeR:    claudeR,
				gptR:       gptR,
				claudeLLM:  claudeRes,
				gptLLM:     gptRes,
				claudeRLLM: claudeRLLM,
				gptRLLM:    gptRLLM,
				claudeRErr: claudeRErr,
				gptRErr:    gptRErr,
			}
			rows = append(rows, r)

			claudeLatTotal += claudeRes.Latency + claudeRLLM.Latency
			gptLatTotal += gptRes.Latency + gptRLLM.Latency
			claudeTokTotal += claudeRes.Usage.Total() + claudeRLLM.Usage.Total()
			gptTokTotal += gptRes.Usage.Total() + gptRLLM.Usage.Total()

			winner := pickWinner(claudeQ.CompositeScore, gptQ.CompositeScore)
			switch winner {
			case "Claude":
				claudeWins++
			case "GPT-4o":
				gptWins++
			default:
				ties++
			}

			t.Logf("[%s daily] Claude=%.2f GPT=%.2f | hist C=%v G=%v | adher C=%v G=%v | avoid C=%v G=%v",
				persona.ID, claudeQ.CompositeScore, gptQ.CompositeScore,
				claudeQ.Persona.HasHistoryCallback, gptQ.Persona.HasHistoryCallback,
				claudeQ.Persona.MentionsAdherence, gptQ.Persona.MentionsAdherence,
				claudeQ.Persona.HitAvoid, gptQ.Persona.HitAvoid,
			)
		})
	}

	printCompareReport(t, cfg, rows, claudeWins, gptWins, ties, claudeLatTotal, gptLatTotal, claudeTokTotal, gptTokTotal)
}

func pickWinner(claude, gpt float64) string {
	const eps = 0.03
	if claude-gpt > eps {
		return "Claude"
	}
	if gpt-claude > eps {
		return "GPT-4o"
	}
	return "Tie"
}

func printCompareReport(
	t *testing.T,
	cfg *config.Config,
	rows []compareRow,
	claudeWins, gptWins, ties int,
	claudeLat, gptLat time.Duration,
	claudeTok, gptTok int,
) {
	t.Helper()
	sep := strings.Repeat("=", 100)
	t.Log(sep)
	t.Log("DaDiary AI Hybrid Compare — Claude Sonnet vs GPT-4o (4 personas, with memory)")
	t.Logf("Models: Claude=%s | GPT=%s | Prompt v%d", cfg.AnthropicModel(), cfg.OpenAITextModel(), CoachDailyPromptVersion)
	t.Log(sep)
	t.Log("")
	t.Log("### Daily Feedback")
	t.Log(fmt.Sprintf("%-22s | %-8s | %-8s | %-8s | %-8s | %-14s | %-14s | %-8s",
		"Persona", "Claude", "GPT-4o", "C ms", "G ms", "C tokens", "G tokens", "Winner"))
	t.Log(strings.Repeat("-", 100))

	for _, r := range rows {
		w := pickWinner(r.claudeQ.CompositeScore, r.gptQ.CompositeScore)
		t.Log(fmt.Sprintf("%-22s | %7.2f | %7.2f | %7d | %7d | %6d in+%4d out | %6d in+%4d out | %s",
			r.personaID,
			r.claudeQ.CompositeScore, r.gptQ.CompositeScore,
			r.claudeLLM.Latency.Milliseconds(), r.gptLLM.Latency.Milliseconds(),
			r.claudeLLM.Usage.InputTokens, r.claudeLLM.Usage.OutputTokens,
			r.gptLLM.Usage.InputTokens, r.gptLLM.Usage.OutputTokens,
			w,
		))
	}

	t.Log("")
	t.Log("### Quality dimensions (daily feedback, with memory)")
	t.Log(fmt.Sprintf("%-22s | %-18s | %-18s | hist | adher | safe | tone | enc",
		"Persona", "Claude", "GPT-4o"))
	t.Log(strings.Repeat("-", 100))
	for _, r := range rows {
		t.Log(fmt.Sprintf("%-22s | C:%5.2f H:%v A:%v | G:%5.2f H:%v A:%v | safe C:%v G:%v | tone C:%v G:%v | enc C:%v G:%v",
			r.personaID,
			r.claudeQ.CompositeScore, r.claudeQ.Persona.HasHistoryCallback, r.claudeQ.Persona.MentionsAdherence,
			r.gptQ.CompositeScore, r.gptQ.Persona.HasHistoryCallback, r.gptQ.Persona.MentionsAdherence,
			r.claudeQ.HasSafety, r.gptQ.HasSafety,
			r.claudeQ.SupportiveTone, r.gptQ.SupportiveTone,
			r.claudeQ.Encouragement, r.gptQ.Encouragement,
		))
	}

	t.Log("")
	t.Log("### Routine Suggest")
	t.Log(fmt.Sprintf("%-22s | %-8s | %-8s | %-8s | %-8s | notes",
		"Persona", "Claude", "GPT-4o", "C ms", "G ms"))
	t.Log(strings.Repeat("-", 100))
	for _, r := range rows {
		note := "ok"
		if r.claudeRErr != nil || r.gptRErr != nil {
			note = fmt.Sprintf("err C=%v G=%v", r.claudeRErr, r.gptRErr)
		}
		t.Log(fmt.Sprintf("%-22s | %7.2f | %7.2f | %7d | %7d | %s",
			r.personaID,
			r.claudeR.CompositeScore, r.gptR.CompositeScore,
			r.claudeRLLM.Latency.Milliseconds(), r.gptRLLM.Latency.Milliseconds(),
			note,
		))
	}

	t.Log("")
	t.Log("### Totals")
	t.Logf("Daily feedback wins: Claude=%d GPT-4o=%d Tie=%d", claudeWins, gptWins, ties)
	t.Logf("Avg latency (both pipelines): Claude=%dms GPT=%dms", claudeLat.Milliseconds()/int64(len(rows)*2), gptLat.Milliseconds()/int64(len(rows)*2))
	t.Logf("Total tokens (both pipelines): Claude=%d GPT=%d", claudeTok, gptTok)

	t.Log("")
	t.Log("### Kết luận")
	if claudeWins > gptWins {
		t.Log("→ Claude Sonnet phù hợp hơn cho DaDiary text coach: cá nhân hóa + history/adherence tốt hơn, tone ấm hơn.")
		t.Log("→ Giữ hybrid: Claude primary, GPT-4o fallback + vision.")
	} else if gptWins > claudeWins {
		t.Log("→ GPT-4o cạnh tranh hoặc hơn trên composite score — xem lại prompt binding trên Claude.")
		t.Log("→ Vẫn nên giữ Claude primary nếu history/adherence cao hơn từng persona.")
	} else {
		t.Log("→ Hòa — cả hai đạt; hybrid strategy vẫn hợp lý (Claude tone, GPT fallback/vision).")
	}
	t.Log(sep)
}

type compareRow struct {
	personaID  string
	pipeline   string
	claudeQ    CoachQualityReport
	gptQ       CoachQualityReport
	claudeR    RoutineQualityReport
	gptR       RoutineQualityReport
	claudeLLM  CompareLLMResult
	gptLLM     CompareLLMResult
	claudeRLLM CompareLLMResult
	gptRLLM    CompareLLMResult
	claudeRErr error
	gptRErr    error
}
