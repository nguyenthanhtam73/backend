# D0/D1 check-in reminders + stale SePay pending orders

## What we found

| Path | Exists today | Used for |
|------|----------------|----------|
| Evening Web Push (`DailyReminderJob`, 20:00 VN) | Yes, if VAPID keys + user subscribed | Anyone who has not checked in *today* (and is not streak-at-risk), skipped if a D0/D1 push already went out the same VN day |
| D0/D1 typed push (`d0_reminder` / `d1_reminder`) | Yes, if VAPID + reminder job enabled | Due `checkin_reminder_flags` with an active push subscription |
| Outbound D0/D1 email (Resend) | Yes, if `RESEND_API_KEY` + `EMAIL_FROM` | ≤1 D0 and ≤1 D1 per user; no-op when ESP env is missing |
| SePay checkout | Yes | Creates `payment_orders` as `pending`; IPN marks `paid` |
| Pending-order cleanup | Yes | Local `pending` → `expired` after TTL |

## 1. D0 / D1 check-in reminder

**Calendar:** `streaktime` (Asia/Ho_Chi_Minh), same as `skin_checks.check_date`.

| Kind | When | Due |
|------|------|-----|
| `d0` | Vietnam civil day the account was created | Active user, no skin check today |
| `d1` | The next Vietnam civil day | Active user, no skin check today |
| `none` | Day 2+ after signup | Never due from this job |

GET recomputes live (so a check-in hides the banner immediately) and upserts `checkin_reminder_flags`. A daily job + boot refresh keep the table current for a later email/push fan-out.

### Frontend

```
GET /api/v1/me/check-in-reminder
Authorization: Bearer <access>
```

```json
{
  "success": true,
  "data": {
    "kind": "d0",
    "due": true,
    "signup_date": "2026-09-05",
    "days_since_signup": 0,
    "checked_in_today": false,
    "channels": {
      "in_app": true,
      "email": true,
      "push_evening": true,
      "push_d0_d1_specific": true,
      "push_note": "d0_d1_specific_enabled"
    }
  }
}
```

`channels.email` is **true only** when Resend is configured (`RESEND_API_KEY` + `EMAIL_FROM`). Otherwise `email: false` and `email_reason: "no_outbound_email"`. `push_d0_d1_specific` is **true when the check-in reminder job is enabled** (`DADIARY_CHECKIN_REMINDER_ENABLED`, default on).

Show an in-app nudge when `due` is true. Do not treat `kind=none` as an error.

### Outbound email (Resend)

The hourly in-process job (same API process — Railway has no separate cron service) refreshes flags then delivers. It claims `push_job_locks.job_name='checkin_reminder_hour'` with an hour key `YYYY-MM-DD-HH` (Vietnam). That key is 13 characters; `last_run_date` must be `VARCHAR(16)` (migration `019`). A `VARCHAR(10)` column makes every hourly claim fail (`SQLSTATE 22001`) and the job never fans out after boot.

On each successful send it writes `email_send_receipts` **before** the Resend POST, then **deletes** the row if Resend rejects. A Resend 403 (`domain is not verified`) therefore leaves the table at 0. The `RESEND_API_KEY` account must have `dadiary.vn` verified — verifying the domain on a different Resend team/account does not count.

Delivery rules:

- ≤1 `d0` and ≤1 `d1` email per user (`email_send_receipts` claim-before-send)
- Skip if `checked_in_today`, inactive, invalid address, or `email_unsubscribed_at` is set
- CTA: `https://dadiary.vn/check-in` (or `DADIARY_PUBLIC_WEB_URL` + `/check-in`). No magic-link auth exists; the web app is auth-aware if the session cookie is present.
- Unsubscribe: `GET|POST /api/v1/email/unsubscribe?token=…` (HMAC with `DADIARY_JWT_SECRET`). `List-Unsubscribe` header is set when `DADIARY_PUBLIC_API_URL` is present.
- Vietnamese copy only; soft DaDiary tone; no diagnosis or cure claims.
- **Missing ESP:** the path no-ops and logs `email no-op — ESP not configured (set RESEND_API_KEY and EMAIL_FROM)`.
- **Unverified From domain:** Resend returns HTTP 403 (`The dadiary.vn domain is not verified`). The claim is released; `email_send_receipts` stays 0. Verify `dadiary.vn` on the **same** Resend account as `RESEND_API_KEY`, then redeploy or `POST /api/v1/admin/check-in-reminders/refresh`.

Railway setup (do not invent keys):

```
RESEND_API_KEY=re_...
EMAIL_FROM=DaDiary <noreply@your-verified-domain>
DADIARY_PUBLIC_API_URL=https://<your-api-host>
DADIARY_PUBLIC_WEB_URL=https://dadiary.vn
```

`DADIARY_RESEND_API_KEY` / `DADIARY_EMAIL_FROM` are aliases.

### D0/D1-specific push

Same hourly pass selects due flags whose user has an active Web Push subscription and sends typed payloads `d0_reminder` / `d1_reminder` (distinct VN copy). Receipts live in `push_send_receipts`. The 20:00 VN `daily_reminder` job skips a user if `daily_reminder`, `d0_reminder`, or `d1_reminder` already has a receipt for that Vietnam civil day — no double-nudge.

### Ops

```bash
go run ./cmd/refresh-checkin-reminders --env .env
go run ./cmd/refresh-checkin-reminders --env .env --apply

# or admin JWT
curl -X POST https://<api>/api/v1/admin/check-in-reminders/refresh \
  -H "Authorization: Bearer <admin-jwt>"
```

API boot also refreshes once (same as billing reconcile).

## 2. Expire leftover pending SePay orders

**Hypothesis (labeled):** repo SePay docs (`docs/SEPAY_DEPLOY_CHECKLIST.md`, `docs/PRODUCTION-CHECKLIST.md`, `internal/usecase/payment/sepay.go`) describe form POST + IPN. They do **not** document a checkout session lifetime. We expire **local** `payment_orders` that stayed `pending` for **72 hours** (override `DADIARY_PENDING_ORDER_TTL_HOURS`, clamped 24–168). We do not call SePay to void the session.

Safety:

- Only `status = pending` rows older than the cutoff are updated → `expired`
- Paid / cancelled / failed rows are untouched
- Re-run is a no-op
- A late `ORDER_PAID` IPN still fulfills (`MarkPaidTx` accepts a non-paid row)

### Ops

```bash
go run ./cmd/expire-pending-orders --env .env
go run ./cmd/expire-pending-orders --env .env --apply

curl -X POST https://<api>/api/v1/admin/payments/expire-pending \
  -H "Authorization: Bearer <admin-jwt>"
```

API boot runs one expire pass; a daily UTC job repeats it.

### Funnel health (admin)

```
GET /api/v1/admin/funnel-stats
Authorization: Bearer <admin-jwt>
```

Counts signups, distinct skin-check users, D0/D1 check-in proxies (Vietnam
calendar), paid orders in the last 7 days, and paywall impressions
(`paywall_views_1d` / `paywall_views_7d`, never `null`). Curl + field table:
[`FUNNEL-STATS.md`](./FUNNEL-STATS.md).

### Verify (Postgres)

```sql
-- leftover pending older than 72h should drop after apply / boot
SELECT count(*) AS stale_pending
FROM payment_orders
WHERE deleted_at IS NULL
  AND status = 'pending'
  AND created_at < now() - interval '72 hours';

SELECT status, count(*)
FROM payment_orders
WHERE deleted_at IS NULL
GROUP BY status
ORDER BY status;

-- D0/D1 flags for today (VN date is stored as date)
SELECT kind, due, count(*)
FROM checkin_reminder_flags
WHERE deleted_at IS NULL
GROUP BY kind, due;
```

## Config

| Env | Default | Meaning |
|-----|---------|---------|
| `DADIARY_CHECKIN_REMINDER_ENABLED` | true | Boot + hourly VN flag refresh and D0/D1 email/push fan-out (in-process; not a Railway cron) |
| `RESEND_API_KEY` / `DADIARY_RESEND_API_KEY` | empty | Resend API key. Empty → email no-op |
| `EMAIL_FROM` / `DADIARY_EMAIL_FROM` | empty | Verified From (e.g. `DaDiary <noreply@dadiary.vn>`). Required with the key |
| `DADIARY_PUBLIC_API_URL` | empty | Public API origin for unsubscribe links |
| `DADIARY_PUBLIC_WEB_URL` | `https://dadiary.vn` | Check-in CTA origin |
| `DADIARY_PENDING_ORDER_EXPIRY_ENABLED` | true | Boot + daily pending expire |
| `DADIARY_PENDING_ORDER_TTL_HOURS` | 72 | Local pending TTL (24–168) |

Schema: `018_email_receipts_and_unsub` + `019_push_job_lock_hour_key` (`last_run_date` VARCHAR(16)). AutoMigrate also applies the widen on API boot. Do not invent `RESEND_API_KEY`.
