# Skin photo accuracy loop

How we make photo reads more accurate on purpose instead of guessing. Read this before
changing anything about how skin photos are described or grouped.

## Why this exists

A skin photo gets turned into a **morphology group** (mụn viêm, mụn ẩn, milia, sần sùi /
texture, thâm, mụn thịt, nếp gấp cổ…). Every routine, tip and product suggestion is
derived from that group, so a wrong group makes everything downstream wrong — and it fails
silently, because the prose still reads confidently.

Before this loop existed there was no way to know how often the group was wrong.

## Where the rules live

| Concern | File |
|---|---|
| Visual taxonomy shared by admin + onboarding + check-in | `internal/service/ai/vision_morphology.go` |
| Group decision tree (testable, no API needed) | `internal/service/ai/morphology_classify.go` |
| Prose → features inference | `internal/service/ai/morphology_prose.go` |
| Label reconciliation, user side | `internal/service/ai/onboarding_morphology_align.go` |
| Label reconciliation, admin side | `internal/service/ai/admin_skin_review_morphology.go` |
| Offline eval (19 labeled cases, runs in CI) | `internal/service/ai/morphology_classify_test.go` |
| Real-photo eval (opt-in) | `internal/service/ai/morphology_eval_live_test.go` |
| Turns operator corrections into labels | `cmd/eval-morphology-export/` |

**Edit `vision_morphology.go` for wording, `morphology_classify.go` for which group wins.**
Never fork those rules into a single pipeline — a lock test fails if you do, because the
whole point is that admin and user flows read a photo the same way.

## The loop

1. **Operators label, just by reviewing.** In `/admin/skin-review`, a wrong group gets
   fixed with "Sửa nhận xét AI". The model's original answer is kept in
   `admin_skin_reviews.analysis_original`; that (original, corrected) pair is one label.
2. **Export the labels** (needs prod DB + R2, see below).
3. **Score** with the real-photo eval. It prints accuracy per group plus every miss.
4. **Fix the cause**, which is usually one of:
   - model named the wrong group on a clear photo → tighten `vision_morphology.go`
   - model described it correctly but the classifier returned `unknown` → a missing phrase
     in the cue lists in `morphology_prose.go`
   - both look right and it still misses → then, and only then, consider a stronger vision
     model (`DADIARY_OPENAI_VISION_MODEL`)
5. **Re-score the same photos** to confirm the number moved. Don't trust "it feels better".

Run it once there are roughly 20–30 corrections. Fewer than that is noise.

## Commands

One-time setup (writes a gitignored env file with prod credentials):

```powershell
cd backend
$v  = railway variables --json | ConvertFrom-Json
$pg = railway variables --service Postgres --json | ConvertFrom-Json
$lines = @(
  "DADIARY_DATABASE_URL=$($pg.DATABASE_PUBLIC_URL)"
  "DADIARY_STORAGE_DRIVER=$($v.DADIARY_STORAGE_DRIVER)"
  "DADIARY_R2_ACCOUNT_ID=$($v.DADIARY_R2_ACCOUNT_ID)"
  "DADIARY_R2_ACCESS_KEY_ID=$($v.DADIARY_R2_ACCESS_KEY_ID)"
  "DADIARY_R2_SECRET_ACCESS_KEY=$($v.DADIARY_R2_SECRET_ACCESS_KEY)"
  "DADIARY_R2_BUCKET=$($v.DADIARY_R2_BUCKET)"
)
[System.IO.File]::WriteAllLines((Join-Path $PWD ".env.prod-eval.local"), $lines, (New-Object System.Text.UTF8Encoding($false)))
```

Use `DATABASE_PUBLIC_URL`, not the backend service's `DADIARY_DATABASE_URL` — that one is
a Railway-internal hostname and is unreachable from a laptop. Write the file **without a
BOM**: `Set-Content -Encoding utf8` adds one in PowerShell 5.1, which corrupts the first
key and silently falls back to the local dev database instead of failing.

Each measurement round:

```powershell
cd backend
go run ./cmd/eval-morphology-export --env .env.prod-eval.local
$v = railway variables --json | ConvertFrom-Json
$env:DADIARY_OPENAI_API_KEY      = $v.DADIARY_OPENAI_API_KEY
$env:DADIARY_OPENAI_VISION_MODEL = $v.DADIARY_OPENAI_VISION_MODEL
go test ./internal/service/ai/ -run TestMorphologyEvalLive -v -count=1
```

`DADIARY_OPENAI_VISION_MODEL` is required: without it the eval scores the default model
instead of the one serving users, which produces a number that looks like evidence while
describing something else.

Re-scoring after a rule change only needs the last line — the exported photos are still
there.

## Privacy

The export writes **real user face photos** to
`internal/service/ai/testdata/morphology/`. The exporter drops a `.gitignore` in that
folder and the path is ignored in `backend/.gitignore`, so it cannot be committed.
Delete the folder when a round is finished:

```powershell
Remove-Item -Recurse -Force internal/service/ai/testdata/morphology
```

Note that `tmp_test_assets/` already contains user photos committed before this rule
existed; gitignoring it does not remove them from history.

## Asking the user questions

The classifier records honest uncertainty in `NeedsMoreInfo`, but a question is only shown
when `ShouldAskUser` agrees — that is, when the look-alikes differ on **care direction**
(needs calming first vs a gentle base). Milia, closed comedones, skin tags and rough
texture all get the same advice, so asking which one it is would dent trust in the read
for no benefit. Keep that gate in place when adding new groups.
