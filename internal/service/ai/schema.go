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
                         History callback when ## Recent SkinChecks present (teasing OK). Sarcastic, hyper-specific.
                         PHOTO_EVIDENCE=skip/limited: MUST include one short chưa-chắc / ảnh-hạn-chế clause; do not lock a morphology group.>,
  "improvements": [
    {
      "tip": <string — ONE concrete actionable step: name the step + body region + product ROLE or action
              ("Tối: rửa mặt dịu vùng má đỏ", "Sáng: kem chống nắng SPF50 vùng thâm").
              BAN vague tips like "sản phẩm nhẹ nhàng" or "chăm sóc nhẹ". Never push >1 new active per check-in.>,
      "why": <string — ONE plain-language clause (2 only if truly needed), confident when PHOTO_EVIDENCE=ok.
              Cite da dễ đỏ, nắng, viêm đang sưng, stress-da, ngủ, thiếu nước — everyday words.
              skip/limited: one short chưa-chắc clause is required, not hedge spam.
              Beginner: skip jargon entirely.>
    }
    // 2–3 items MAX (both modes) — pick the highest-impact steps, don't pad.
  ],
  "care_suggestions": [
    {
      "slot": <"morning"|"evening"|"today" — group for UI. Prefer morning/evening when a step is time-bound; use "today" for priority avoid/do once.>,
      "step": <string — everyday step NAME only, no brand: "Rửa mặt dịu", "Dưỡng ẩm", "Chống nắng", "Giảm active mạnh". EN: "Gentle cleanse", "Moisturize", "SPF".>,
      "why": <string — ONE sentence: why this fits TODAY. Name an owned ## Wardrobe product when that role is already on the shelf. Confident when PHOTO_EVIDENCE=ok; one short chưa-chắc clause when skip/limited.>,
      "safety_note": <string — optional short caution: avoid picking, ease strong actives if inflamed, patch-test if new, see derm if large/painful/lasting. Empty string if N/A.>
    }
    // 3–5 items. IN-APP ONLY detailed care (richer than public share 2–3 soothing_tips).
    // BAN: hard disease names, prescription drugs/antibiotics, mandatory brand names, hedge spam.
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
                        (or diverge) from vision cues. When vision is available and PHOTO_EVIDENCE=ok,
                        include at least 1 additional photo-specific detail not repeated verbatim
                        from situation_analysis. PHOTO_EVIDENCE=skip: say coaching is from tags/notes only.
                        PHOTO_EVIDENCE=limited: MUST say ảnh hạn chế / chưa chắc. No hard disease names.>,
  "medical_disclaimer": <string — ONE short closing line: informational coaching only,
                         not medical diagnosis or treatment, not a substitute for a clinician.
                         Match the user's language (VI if notes/tags Vietnamese; EN otherwise).
                         Do NOT paste this hedge into every other field.>,
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

// coachOutputJSONSchemaBlockAndroid is the Play-store schema block. Same keys,
// caps, and evidence rules as CoachOutputJSONSchemaBlock; only the voice examples change.
const coachOutputJSONSchemaBlockAndroid = `Required JSON schema (every top-level key MUST appear — use [] / "" / 0 only when truly N/A):
{
  "score": <number 0–1 — soft “how supported / on-track TODAY feels” from context + habits.
            NEVER a guilt, beauty, or moral grade. Avoid extreme 0/1 unless clearly justified.>,
  "strengths": [<string> — 1–4 genuine praise bullets tied to TODAY effort (journaling, photos, context).
                 Polite mình/bạn tone: warm and specific — never sarcastic, crude, or cold/clinical.
                 When USER_MEMORY has ## Routine adherence, ≥1 bullet MUST acknowledge routine effort per COACH_ACTION
                 (praise consistency / validate low ticks / encourage restart — never guilt).
                 Beginner mode: 1–3. NEVER flattery about appearance.>],
  "situation_analysis": <string — 2–3 sentences ONLY, TIGHT (no filler, no restating tags). MUST open with "Mình thấy hôm nay…",
                         "Hôm nay da bạn…", "Cái vùng … hôm nay…", "Trên ảnh mình thấy vùng …", or "Có … nốt mụn/chấm thâm ở …".
                         Weave ≥3–4 photo-specific details (region + cue + degree/count) — specificity matters more than length;
                         pack the details into the 2–3 sentences rather than adding more sentences.
                         BAN: "da hỗn hợp", "da dễ nổi mụn", vague dryness without region. BAN: tao/mày, profanity, crude slang.
                         History callback when ## Recent SkinChecks present (warm, specific — no teasing). Polite, hyper-specific.
                         PHOTO_EVIDENCE=skip/limited: MUST include one short chưa-chắc / ảnh-hạn-chế clause; do not lock a morphology group.>,
  "improvements": [
    {
      "tip": <string — ONE concrete actionable step: name the step + body region + product ROLE or action
              ("Tối: rửa mặt dịu vùng má đỏ", "Sáng: kem chống nắng SPF50 vùng thâm").
              BAN vague tips like "sản phẩm nhẹ nhàng" or "chăm sóc nhẹ". Never push >1 new active per check-in.>,
      "why": <string — ONE plain-language clause (2 only if truly needed), confident when PHOTO_EVIDENCE=ok.
              Cite da dễ đỏ, nắng, viêm đang sưng, stress-da, ngủ, thiếu nước — everyday words.
              skip/limited: one short chưa-chắc clause is required, not hedge spam.
              Beginner: skip jargon entirely.>
    }
    // 2–3 items MAX (both modes) — pick the highest-impact steps, don't pad.
  ],
  "care_suggestions": [
    {
      "slot": <"morning"|"evening"|"today" — group for UI. Prefer morning/evening when a step is time-bound; use "today" for priority avoid/do once.>,
      "step": <string — everyday step NAME only, no brand: "Rửa mặt dịu", "Dưỡng ẩm", "Chống nắng", "Giảm active mạnh". EN: "Gentle cleanse", "Moisturize", "SPF".>,
      "why": <string — ONE sentence: why this fits TODAY. Name an owned ## Wardrobe product when that role is already on the shelf. Confident when PHOTO_EVIDENCE=ok; one short chưa-chắc clause when skip/limited.>,
      "safety_note": <string — optional short caution: avoid picking, ease strong actives if inflamed, patch-test if new, see derm if large/painful/lasting. Empty string if N/A.>
    }
    // 3–5 items. IN-APP ONLY detailed care (richer than public share 2–3 soothing_tips).
    // BAN: hard disease names, prescription drugs/antibiotics, mandatory brand names, hedge spam.
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
                        (or diverge) from vision cues. When vision is available and PHOTO_EVIDENCE=ok,
                        include at least 1 additional photo-specific detail not repeated verbatim
                        from situation_analysis. PHOTO_EVIDENCE=skip: say coaching is from tags/notes only.
                        PHOTO_EVIDENCE=limited: MUST say ảnh hạn chế / chưa chắc. No hard disease names.>,
  "medical_disclaimer": <string — ONE short closing line: informational coaching only,
                         not medical diagnosis or treatment, not a substitute for a clinician.
                         Match the user's language (VI if notes/tags Vietnamese; EN otherwise).
                         Do NOT paste this hedge into every other field.>,
  "summary_notes": <string — ≤2 sentences: ONE polite closing (encouraging, mình/bạn, no sarcasm or profanity) + ONE concrete focus for tomorrow's check-in.
                    E.g. "Mai chụp cùng góc nhé — mình muốn xem vùng đó dịu hơn không." No report tone, emoji floods, or platitudes.>,` + ProductSuggestionsJSONField + `
}

Strict output rules:
- Output EXACTLY ONE JSON object. No markdown, no code fences, no text before or after.
- BREVITY (HARD): keep every string tight and skimmable — no filler, no preamble, never repeat a detail across fields. Respect the per-field caps above: situation_analysis 2–3 sentences, improvements 2–3 items, care_suggestions 3–5 items, routine_hints 3–4 lines (Beginner 2–3), safety_reminders 1–2 lines, concern_alignment 1–2 sentences. Shorter output = faster response; specific-and-short beats long-and-generic.
- JSON keys MUST use the exact ASCII spellings above.
- "routine_hints": every line MUST be prefixed. Never leave a hint unprefixed (the UI splits cards by prefix).
- Match USER_INTERFACE_LOCALE (vi or en) for ALL human-readable string values when present.
- Voice: polite mình/bạn only. Never tao/mày, never profanity or crude slang, in every mode including Beginner.
- If a context block (vision / profile / diary) is missing, simply omit references to it — do not invent details.`

// coachOutputSchemaForClient returns the user-message schema block for a skin check.
// Web (and any non-android kind) is CoachOutputJSONSchemaBlock, unchanged.
func coachOutputSchemaForClient(clientKind string) string {
	if IsAndroidCoachVoice(clientKind) {
		return coachOutputJSONSchemaBlockAndroid
	}
	return CoachOutputJSONSchemaBlock
}

// VisionObservationSchemaBlock constrains GPT vision to conservative, non-diagnostic JSON.
// Fields are intentionally terse: vision runs in parallel with memory but feeds the coach,
// so shorter observations cut vision generation time AND shrink the coach's input prompt
// (faster time-to-first-token) without losing the region + cue detail the coach relies on.
const VisionObservationSchemaBlock = `Return ONE JSON object only (no markdown). Keep every field short — one phrase/sentence each. Schema:
{
  "photo_assessment": {
    "lighting": <string — a few words>,
    "angle_clarity": <string — a few words>,
    "limitations": <string — only if blurry/cropped/badly lit blocks a cue; else "">
  },
  "visible_observations": [<string — ≤5 short confident bullets when signs are clear; region + CORRECT morphology group + degree; e.g. mụn ẩn / milia / sần sùi / thâm quanh miệng / mụn thịt (neck only); do not invent hard disease names>],
  "zone_observations": [
    {
      "zone": <"forehead"|"nose"|"left_cheek"|"right_cheek"|"chin"|"around_mouth"|"jawline"|"under_eyes"|"neck"|"other" — only a zone actually visible in the photo. Never invent a zone.>,
      "cue": <short plain phrase: what that zone looks like + morphology group + degree. Use "trông giống" for look-alikes. No hard disease names.>,
      "severity": <"mild"|"moderate"|"pronounced" — how visible / strong the sign is>
    }
  ],
  "texture_and_oil_cues": <string — one short sentence>,
  "redness_or_discoloration_cues": <string — one short sentence>,
  "uncertainty_note": <string — only when photo truly limits reading; else empty or one short clause>
}
zone_observations: max 5. If a hint says an image is a CLOSE-UP of one zone, only describe that zone.`

// CheckInDetailJSONFields is appended only to the photo check-in coach user message.
// Daily feedback and other prompts do not include it. No voice words: the system
// prompt already sets web vs polite Android voice.
const CheckInDetailJSONFields = `Also include these photo check-in keys:
{
  "zone_notes": [
    {
      "zone": <"forehead"|"nose"|"left_cheek"|"right_cheek"|"chin"|"around_mouth"|"jawline"|"under_eyes"|"neck"|"other" — ONLY a zone listed in VISION zone_observations or in the close-up photo meta. Never invent a zone that is not visible.>,
      "note": <ONE short sentence. Say only what the skin looks like, using "trông giống" (VI) or "looks like" (EN). Never name a disease or give a diagnosis. If the sentence would name a disease, invent a zone, or otherwise break this rule, omit this WHOLE item. Do not shorten the sentence to hide the problem.>,
      "severity": <"mild"|"moderate"|"pronounced" — how visible the sign is>
    }
  ],
  "skin_score_notes": {
    "overall": <ONE short sentence: why the overall score is where it is today. Cite one photo or tag cue. Add a short "chưa chắc" clause when PHOTO_EVIDENCE is limited or skip.>,
    "hydration": <ONE short sentence: why the hydration score is where it is today. Same rules as overall.>,
    "clarity": <ONE short sentence: why the clarity score is where it is today. Same rules as overall.>,
    "barrier": <ONE short sentence: why the barrier score is where it is today. Same rules as overall.>
  }
}
zone_notes: max 5, one short sentence each. Empty array when PHOTO_EVIDENCE=skip.
skin_score_notes: all 4 keys, including overall. One short sentence each. Do not put these sentences under the numeric skin_scores keys.`

// DefaultMedicalDisclaimerVI used when the model omits an explicit disclaimer.
const DefaultMedicalDisclaimerVI = "Đây chỉ là gợi ý tham khảo từ ảnh/check-in — không thay thế tư vấn bác sĩ da liễu."

// DefaultMedicalDisclaimerEN is the English fallback disclaimer.
const DefaultMedicalDisclaimerEN = "This is informational guidance from today's check-in — not a substitute for a dermatologist's advice."
