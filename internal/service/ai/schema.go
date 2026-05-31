package ai

// CoachOutputJSONSchemaBlock is appended to the user message so the model returns parseable JSON.
// Field semantics align with the Daily Check-in UI: praise → today summary → AM/PM hints → tips → safety + disclaimer.
const CoachOutputJSONSchemaBlock = `Required JSON schema (every top-level key MUST appear — use [] / "" / 0 only when truly N/A):
{
  "score": <number 0–1 — soft “how supported / on-track TODAY feels” from context + habits.
            NEVER a guilt, beauty, or moral grade. Avoid extreme 0/1 unless clearly justified.>,
  "strengths": [<string> — 1–4 genuine praise bullets tied to TODAY effort (journaling, photos, context).
                 When USER_MEMORY has ## Routine adherence, ≥1 bullet MUST acknowledge routine effort per COACH_ACTION
                 (praise consistency / validate low ticks / encourage restart — never guilt).
                 Beginner mode: 1–3. NEVER flattery about appearance.>],
  "situation_analysis": <string — 2–7 sentences (Beginner: 2–4). Lead with TODAY skin + environment.
                         When VISION_SUMMARY_JSON is present (not unavailable): cite ≥3 photo-specific
                         details (body region + visible cue — e.g. "T-zone bóng dầu", "4 nốt đỏ ở cằm",
                         "má trái hơi khô"). NEVER use vague-only lines like "da hơi khô" without region.
                         When USER_MEMORY has ## Recent SkinChecks, include ≥1 history-callback sentence
                         ("mấy lần gần đây…", "vài hôm trước…", "so với lần trước…") comparing pattern to today.
                         Merge vision + tags + notes. No diagnosis.>,
  "improvements": [
    {
      "tip": <string — ONE small actionable step for today or tonight. Generic product ROLE
              only ("kem dưỡng dày hơn", "lotion humectant") unless user already named brand
              or active. Never push more than ONE new product or active per check-in.>,
      "why": <string — 1–2 plain-language clauses. Cite barrier, sun, inflammation pacing,
              stress-skin, sleep, hydration. Beginner: skip jargon entirely.>
    }
    // 2–3 items for Beginner mode; 2–5 for Normal mode.
  ],
  "routine_hints": [<string> — EVERY line MUST start with "Sáng:" or "Tối:" (VI) or "AM:" / "PM:" (EN).
                     When USER_MEMORY ## Routine adherence COACH_ACTION says low/none: cap at 2–3 lines total.
                     Beginner: 2–4 total; Normal: 3–6 total.>],
  "avoid_or_patch": [<string> — what to ease off / patch-test / not stack today.
                      Always include a patch-test reminder when user mentions any new product.>],
  "safety_reminders": [<string> — SPF reapply habit, one-change-at-a-time rule, when to seek
                        in-person care. If user mentions red-flag symptoms (fever, swelling,
                        oozing, severe burning, painful rapidly-worsening rash, eye/lip involvement,
                        or duration > 6 weeks) include a clear "đến gặp bác sĩ da liễu" line.>],
  "skin_scores": {
    "hydration": <0–1>,
    "clarity":   <0–1>,
    "barrier":   <0–1>
    // Soft gauges from TODAY context only — not clinical. Use mid-range unless context is strong.
  },
  "concern_alignment": <string — ONE short paragraph: how the user's TODAY tags line up
                        (or gently diverge) from soft vision cues. When vision is available,
                        include at least 1 additional photo-specific detail not repeated verbatim
                        from situation_analysis. Always acknowledge uncertainty ("ảnh chỉ là một góc thôi").
                        No diagnosis.>,
  "medical_disclaimer": <string — Must clearly say this is informational coaching only,
                         not medical diagnosis or treatment, and not a substitute for a clinician.
                         Match the user's language (VI if notes/tags Vietnamese; EN otherwise).>,
  "summary_notes": <string — ONE warm closing sentence + ONE concrete focus for tomorrow's check-in.
                    Example (VI): "Mai chụp cùng góc ánh sáng nhé — mình muốn xem má bạn dịu lại tới đâu."
                    No emoji floods. No "stay strong!" platitudes.>
}

Strict output rules:
- Output EXACTLY ONE JSON object. No markdown, no code fences, no text before or after.
- JSON keys MUST use the exact ASCII spellings above.
- "routine_hints": every line MUST be prefixed. Never leave a hint unprefixed (the UI splits cards by prefix).
- Match USER_INTERFACE_LOCALE (vi or en) for ALL human-readable string values when present.
- If a context block (vision / profile / diary) is missing, simply omit references to it — do not invent details.`

// VisionObservationSchemaBlock constrains GPT vision to conservative, non-diagnostic JSON.
const VisionObservationSchemaBlock = `Return ONE JSON object only (no markdown). Schema:
{
  "photo_assessment": {
    "lighting": <string>,
    "angle_clarity": <string>,
    "limitations": <string — what a single photo cannot prove>
  },
  "visible_observations": [<string — short conservative bullets; describe, do not diagnose>],
  "texture_and_oil_cues": <string>,
  "redness_or_discoloration_cues": <string>,
  "uncertainty_note": <string — remind limits of photo-only reading>
}`

// DefaultMedicalDisclaimerVI used when the model omits an explicit disclaimer.
const DefaultMedicalDisclaimerVI = "Đây chỉ là gợi ý tham khảo dựa trên thông tin bạn cung cấp. Không thay thế tư vấn bác sĩ da liễu."

// DefaultMedicalDisclaimerEN is the English fallback disclaimer.
const DefaultMedicalDisclaimerEN = "This is informational guidance only — not a substitute for a dermatologist's advice."
