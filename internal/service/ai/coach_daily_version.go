package ai

// CoachDailyPromptVersion increments when the daily check-in coach system prompt or JSON semantics change materially.
// Stored on SkinAnalysis.PromptVersion after a successful pipeline run for debugging and compatibility.
//
// v9 (2026-05-14) — Added explicit "## Cá nhân hoá theo USER_MEMORY" section
// to coachCorePromptVI. Coach now MUST surface at least one of 4 callback
// patterns (trend callback / pivot away from 👎 / repeat 👍 / adherence-tuned
// suggestion difficulty) whenever USER_MEMORY has data. Schema unchanged.
//
// v10 (2026-05-30) — Optimised persona + compact user memory block.
//
// v11 (2026-05-30) — Stronger USER_MEMORY binding + adherence-driven routine sizing.
// v12 (2026-05-30) — Refinement from live persona tests: mandatory 1–2 history
// callbacks, explicit adherence mention, richer Beginner examples. Schema unchanged.
//
// v14 (2026-05-31) — Vision-first coaching: ≥3 photo-specific citations mandatory
// when VISION_SUMMARY_JSON is present; ban generic dryness/uneven-tone phrases;
// explicit 6-step structure (praise → observe → compare → suggest → safety → disclaimer).
//
// v15 (2026-05-31) — Natural conversational tone: less report-like phrasing, warmer
// encouragement at open/close, smoother 6-part flow; keeps v14 vision + history enforcement.
//
// v16 (2026-05-31) — Best-friend tone: ≥3–4 vision details, mandatory conversational
// openers ("mình thấy"), stronger anti-report/list enforcement, 6 scenarios QA.
const CoachDailyPromptVersion = 16
