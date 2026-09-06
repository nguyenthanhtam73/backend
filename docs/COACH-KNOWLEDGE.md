# Coach public knowledge pack

Internal, curated notes the **daily Coach** (check-in + text-only feedback) can use for three themes we already answer often:

1. Irritation / weak surface layer after **adapalene** or **BHA**
2. Post-acne marks vs **melasma** (cautious — no diagnosis)
3. Oily skin / **closed comedones** — slow BHA, no extraction

This is **not** RAG over user diaries, Facebook, Admin Skin Review photos, or selling data. Every claim is traced to a public AAD, DermNet, NCBI StatPearls, or DailyMed page listed in the JSON.

## Where it lives

| Piece | File |
|---|---|
| Curated pack + citations | `internal/service/ai/coach_knowledge.json` |
| Load, keyword match, prompt render | `internal/service/ai/coach_knowledge.go` |
| Always-on system-prompt pointer | `CoachPublicKnowledgeGuard()` inside `GetCoachPrompt` |
| Theme injection (user message) | `AppendCoachKnowledgeContext` in `daily_feedback.go` and `pipeline_skin_check.go` |
| How to update (this page) | `docs/COACH-KNOWLEDGE.md` |
| Offline tests / eval needles | `internal/service/ai/coach_knowledge_test.go` |

Prompt version: bump `CoachDailyPromptVersion` in `coach_daily_version.go` when the pack wording or match rules change materially (currently **27**).

The affiliate catalog is a separate embed. Knowledge is injected **before** affiliate rules so calm-first / no-extraction wins over product picks.

## How the Coach uses it

1. System prompt always has a short compass (`CoachPublicKnowledgeGuard`): no diagnosis, no picking, calm + sunscreen + when to see a doctor.
2. User message gets `## COACH_KNOWLEDGE` **only** when today's note, profile, or vision JSON matches a theme. Unrelated check-ins stay token-light.
3. Coach-facing Vietnamese; do **not** dump URLs or English jargon at the user. `medical_disclaimer` stays the existing one-liner.

## How to update a theme

1. Open a public source (prefer AAD patient pages, then DermNet, then an official monograph). Read the page. Do not invent timelines, percents, or diagnoses the page does not state.
2. Edit **only** `coach_knowledge.json`:
   - `summary_vi` / `do_vi` / `dont_vi` / `see_doctor_vi` — easy Vietnamese.
   - `match_any` / `match_pairs` — keywords the user or vision JSON might use (include a no-diacritic variant when people type without dấu).
   - `sources[]` — `name`, `title` (what the page actually says), `url` (https), `accessed` (YYYY-MM-DD).
3. Allowed hosts (enforced in tests): `aad.org`, `dermnetnz.org`, `ncbi.nlm.nih.gov`, `dailymed.nlm.nih.gov`.
4. Add or adjust a line in `TestMatchCoachKnowledgeThemes` and `TestCoachKnowledgeEvalNeedles` using a real phrasing you expect.
5. Run:

```bash
go test ./internal/service/ai -count=1 -short -run 'TestCoachKnowledge|TestCoachPrompt_IncludesPublicKnowledgeGuard|TestCoachPromptVersion'
```

6. Bump `CoachDailyPromptVersion` if the Coach will see different instructions.

Do **not** paste user check-in text, Facebook comments, or skin-review notes into the pack. If a live miss is a wording gap, add a **public** phrase to `match_*` or tighten the Vietnamese care lines — same idea as the morphology cue lists, but this file is care guidance, not photo grouping.

## Offline eval (no API key)

`TestCoachKnowledgeEvalNeedles` pins three sample user lines → theme + required care needles:

| User line | Theme | Must appear in the injected block |
|---|---|---|
| Mới dùng adapalene 4 đêm, má bong và rát | irritation_after_adapalene_bha | dưỡng ẩm, chống nắng, cách ngày, khám |
| Thâm sau mụn hay nám má vậy ạ | post_acne_marks_vs_melasma | không chốt, chống nắng, thâm sau mụn, khám |
| Da dầu, mụn ẩn dày, tối nay nặn cho sạch | oily_closed_comedones | CẤM nặn, BHA, chậm, khám |

That is enough to catch a pack regression without OpenAI spend. Optional live check (founder, existing coach live tests): after deploy, send those three notes through check-in / daily feedback and confirm the JSON tips follow the pack (calm / no diagnosis / no extraction).

## What this is not

- Not a diagnosis engine. `GroupUnknown` and photo grouping still live in `vision_morphology.go`.
- Not Admin Skin Review public-reply frames (those stay in `admin_skin_review_suggest_answer.go`).
- Not a place for brands, prices, or SePay/Resend config.
