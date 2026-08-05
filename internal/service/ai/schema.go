package ai

// CoachOutputJSONSchemaBlock is appended to the user message so the model returns parseable JSON.
// Field semantics align with the Daily Check-in UI: praise → today summary → AM/PM hints → tips → safety + disclaimer.
const CoachOutputJSONSchemaBlock = `Required JSON schema (every top-level key MUST appear — use [] / "" / 0 only when truly N/A):
{
  "score": <number 0–1 — soft “how supported / on-track TODAY feels” from context + habits.
            NEVER a guilt, beauty, or moral grade. Avoid extreme 0/1 unless clearly justified.>,
  "strengths": [<string> — 1–4 genuine praise OR playful roast bullets tied to TODAY effort (journaling, photos, context).
                 Gen Z buddy tone: mỉa mai nhẹ + châm chọc OK, still caring — never cold/clinical.
                 When USER_MEMORY has ## Routine adherence, ≥1 bullet MUST acknowledge routine effort per COACH_ACTION
                 (praise consistency / validate low ticks / encourage restart — never guilt).
                 Beginner mode: 1–3. NEVER flattery about appearance.>],
  "situation_analysis": <string — 2–3 sentences ONLY, TIGHT (no filler, no restating tags). MUST open with "Mày thấy hôm nay…",
                         "Đm da mày hôm nay…", "Cái vùng … hôm nay…", "Trên ảnh tao thấy vùng …", or "Có … nốt mụn/chấm thâm ở …".
                         Weave ≥3–4 photo-specific details (region + cue + degree/count) — specificity matters more than length;
                         pack the details into the 2–3 sentences rather than adding more sentences.
                         BAN: "da hỗn hợp", "da dễ nổi mụn", vague dryness without region.
                         History callback when ## Recent SkinChecks present (teasing OK). Sarcastic, hyper-specific.>,
  "improvements": [
    {
      "tip": <string — ONE concrete actionable step: name the step + body region + product ROLE or action
              ("Tối: rửa mặt dịu vùng má đỏ", "Sáng: kem chống nắng SPF50 vùng thâm").
              BAN vague tips like "sản phẩm nhẹ nhàng" or "chăm sóc nhẹ". Never push >1 new active per check-in.>,
      "why": <string — ONE plain-language clause (2 only if truly needed). Cite barrier, sun, inflammation pacing,
              stress-skin, sleep, hydration. Beginner: skip jargon entirely.>
    }
    // 2–3 items MAX (both modes) — pick the highest-impact steps, don't pad.
  ],
  "care_suggestions": [
    {
      "slot": <"morning"|"evening"|"today" — group for UI. Prefer morning/evening when a step is time-bound; use "today" for priority avoid/do once.>,
      "step": <string — everyday step NAME only, no brand: "Rửa mặt dịu", "Dưỡng ẩm", "Chống nắng", "Giảm active mạnh". EN: "Gentle cleanse", "Moisturize", "SPF".>,
      "why": <string — ONE sentence: why this fits TODAY's photo/tags (region + cue).>,
      "safety_note": <string — optional short caution: avoid picking, ease strong actives if inflamed, patch-test if new, see derm if large/painful/lasting. Empty string if N/A.>
    }
    // 3–5 items. IN-APP ONLY detailed care (richer than public share 2–3 soothing_tips).
    // BAN: hard diagnosis, prescription drugs/antibiotics, mandatory brand names.
    // Do NOT invent a full multi-product AM–PM shelf routine — light checklist only.
  ],
  "routine_hints": [<string> — EVERY line MUST start with "Sáng:" or "Tối:" (VI) or "AM:" / "PM:" (EN). Keep each line to one short step.
                     When USER_MEMORY ## Routine adherence COACH_ACTION says low/none: cap at 2–3 lines total.
                     Beginner: 2–3 total; Normal: 3–4 total.
                     These stay short apply-to-today lines; put richer why/safety in care_suggestions.>],
  "avoid_or_patch": [<string> — what to ease off / patch-test / not stack today.
                      Always include a patch-test reminder when user mentions any new product.>],
  "safety_reminders": [<string> — 1–2 short lines only: SPF reapply habit, one-change-at-a-time rule, when to seek
                        in-person care. If user mentions red-flag symptoms (fever, swelling,
                        oozing, severe burning, painful rapidly-worsening rash, eye/lip involvement,
                        or duration > 6 weeks) include a clear "đến gặp bác sĩ da liễu" line.>],
  "skin_scores": {
    "hydration": <0–1>,
    "clarity":   <0–1>,
    "barrier":   <0–1>
    // Soft gauges from TODAY context only — not clinical. Use mid-range unless context is strong.
  },
  "concern_alignment": <string — 1–2 short sentences: how the user's TODAY tags line up
                        (or gently diverge) from soft vision cues. When vision is available,
                        include at least 1 additional photo-specific detail not repeated verbatim
                        from situation_analysis. Always acknowledge uncertainty ("ảnh chỉ là một góc thôi").
                        No diagnosis.>,
  "medical_disclaimer": <string — Must clearly say this is informational coaching only,
                         not medical diagnosis or treatment, and not a substitute for a clinician.
                         Match the user's language (VI if notes/tags Vietnamese; EN otherwise).>,
  "summary_notes": <string — ≤2 sentences: ONE buddy closing (encouraging, may still be mildly sarcastic) + ONE concrete focus for tomorrow's check-in.
                    E.g. "Mai chụp cùng góc nhé con — tao muốn xem mày có chịu làm không." No report tone, emoji floods, or platitudes.>,` + ProductSuggestionsJSONField + `
}

Strict output rules:
- Output EXACTLY ONE JSON object. No markdown, no code fences, no text before or after.
- BREVITY (HARD): keep every string tight and skimmable — no filler, no preamble, never repeat a detail across fields. Respect the per-field caps above: situation_analysis 2–3 sentences, improvements 2–3 items, care_suggestions 3–5 items, routine_hints 3–4 lines (Beginner 2–3), safety_reminders 1–2 lines, concern_alignment 1–2 sentences. Shorter output = faster response; specific-and-short beats long-and-generic.
- JSON keys MUST use the exact ASCII spellings above.
- "routine_hints": every line MUST be prefixed. Never leave a hint unprefixed (the UI splits cards by prefix).
- Match USER_INTERFACE_LOCALE (vi or en) for ALL human-readable string values when present.
- If a context block (vision / profile / diary) is missing, simply omit references to it — do not invent details.`

// VisionObservationSchemaBlock constrains GPT vision to conservative, non-diagnostic JSON.
// Fields are intentionally terse: vision runs in parallel with memory but feeds the coach,
// so shorter observations cut vision generation time AND shrink the coach's input prompt
// (faster time-to-first-token) without losing the region + cue detail the coach relies on.
const VisionObservationSchemaBlock = `Return ONE JSON object only (no markdown). Keep every field short — one phrase/sentence each. Schema:
{
  "photo_assessment": {
    "lighting": <string — a few words>,
    "angle_clarity": <string — a few words>,
    "limitations": <string — what a single photo cannot prove, one short clause>
  },
  "visible_observations": [<string — ≤5 short conservative bullets; region + cue; describe, do not diagnose>],
  "texture_and_oil_cues": <string — one short sentence>,
  "redness_or_discoloration_cues": <string — one short sentence>,
  "uncertainty_note": <string — one short clause reminding limits of photo-only reading>
}`

// DefaultMedicalDisclaimerVI used when the model omits an explicit disclaimer.
const DefaultMedicalDisclaimerVI = "Đây chỉ là gợi ý tham khảo dựa trên thông tin bạn cung cấp. Không thay thế tư vấn bác sĩ da liễu."

// DefaultMedicalDisclaimerEN is the English fallback disclaimer.
const DefaultMedicalDisclaimerEN = "This is informational guidance only — not a substitute for a dermatologist's advice."
