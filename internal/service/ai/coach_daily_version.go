package ai

// CoachDailyPromptVersion increments when the daily check-in coach system prompt or JSON semantics change materially.
// Stored on SkinAnalysis.PromptVersion after a successful pipeline run for debugging and compatibility.
//
// v9 (2026-05-14) — Added explicit "## Cá nhân hoá theo USER_MEMORY" section
// to coachCorePromptVI. Coach now MUST surface at least one of 4 callback
// patterns (trend callback / pivot away from 👎 / repeat 👍 / adherence-tuned
// suggestion difficulty) whenever USER_MEMORY has data. Schema unchanged.
//
// v10 (2026-05-30) — Optimised persona + 6-step contract; stronger USER_MEMORY
// emphasis; compact feedback summary + routine adherence in memory block;
// default VI disclaimer shortened. Schema unchanged.
const CoachDailyPromptVersion = 10
