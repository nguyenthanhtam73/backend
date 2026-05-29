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
const CoachDailyPromptVersion = 12
