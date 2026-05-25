// Package ai is the single integration layer for DaDiary LLM providers.
//
// Text coaching (Claude primary; OpenAI JSON fallback when Anthropic is unset):
//   - GenerateStarterRoutine — onboarding AM/PM starter + warm copy (starter_routine.go).
//   - GenerateDailyFeedback — structured coach JSON without vision (daily_feedback.go).
//   - RunSkinCheckCoach — GPT vision observations → Claude coach JSON; SkinProfile + USER_MEMORY enrich USER_CONTEXT (pipeline_skin_check.go, checkin_context.go).
//   - GenerateSuggestedRoutine — repeatable AM/PM routine builder (suggest_routine.go); also consumes USER_MEMORY.
//
// Personalisation:
//   - BuildUserMemoryContext (user_memory.go) — single helper that gathers
//     SkinProfile + last 5–8 SkinChecks (with prior AI summary lines) +
//     thumbs-up/down votes + routine adherence + (when total checks > 50)
//     a per-month digest of older history into one prompt block. Wired into
//     RunSkinCheckCoach, GenerateSuggestedRoutine, and the re-onboarding
//     branch of GenerateStarterRoutine so the coach feels like the same
//     friend across days.
//   - MemoryCache (user_memory_cache.go) — in-process TTL+cap cache (default
//     5 min, 10k entries) shared across services. Each write path
//     (skincheck.Create → analysis.Process, routine.Upsert, aifeedback.Create,
//     profile.PutSkin / CompleteOnboarding) busts the user's entry so AI
//     calls always see the latest state.
//   - GET /api/v1/me/memory — read-only debug endpoint backed by
//     internal/usecase/usermemory. Returns the same block + diagnostic
//     stats (char count, total checks, cache size). Pass ?fresh=1 to
//     bypass the TTL cache.
//
// Vision / photos (OpenAI):
//   - Onboarding skin analyze, skin-check observation pass (onboarding_skin.go, vision_openai.go).
//
// Environment: DADIARY_OPENAI_* (vision required for photo pipelines), DADIARY_ANTHROPIC_* (recommended for coaching quality).
// Prompts: coach personas + skill routing in coach_prompt.go (GetCoachPrompt, ResolveCoachSkillLevel);
// Claude-specific stubs and other prompts in prompts_claude.go, prompts_onboarding.go, schema.go.
package ai
