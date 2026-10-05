# D0/D1 check-in reminders + stale SePay pending orders

## What we found

| Path | Exists today | Used for |
|------|----------------|----------|
| Evening Web Push (`DailyReminderJob`, 20:00 VN) | Yes, if VAPID keys + user subscribed | Anyone who has not checked in *today* (and is not streak-at-risk), skipped if a D0/D1 push already went out the same VN day |
| D0/D1 typed push (`d0_reminder` / `d1_reminder`) | Yes, if VAPID + reminder job enabled | Due `checkin_reminder_flags` with an active push subscription |
| Outbound D0 email (Resend) | Yes, if `RESEND_API_KEY` + `EMAIL_FROM` | Hourly pass. ≤1 D0 per user. Same-day signup, no check-in yet |
| Outbound D1 / Day-3 email (Resend) | Yes, same ESP | 19:30 Asia/Ho_Chi_Minh. Anchored on first check-in, not signup. See below |
| SePay checkout | Yes | Creates `payment_orders` as `pending`; IPN marks `paid` |
| Pending-order cleanup | Yes | Local `pending` → `expired` after TTL |

## 1. D0 / D1 check-in reminder

**Calendar:** `streaktime` (Asia/Ho_Chi_Minh), same as `skin_checks.check_date`.

| Kind | When | Due |
|------|------|-----|
| `d0` | Vietnam civil day the account was created | Active user, no skin check today. In-app banner and the hourly email |
| `d1` | The next Vietnam civil day after signup | Active user, no skin check today. In-app banner and hourly D1 **push** only. The D1 **email** is not this row — see evening email |
| `none` | Day 2+ after signup | Never due from the signup-anchored job |

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

Two in-process jobs (Railway has no separate cron service):

1. **Hourly** — refresh signup-anchored flags, send ≤1 `d0` email, and send typed D0/D1 push. Does **not** send the D1 email (that used to go out on the first tick after VN midnight).
2. **19:30 Asia/Ho_Chi_Minh** (`evening_checkin_email_job`, checks every 30 minutes, once per VN civil day, including a restart after 19:30). Sends:

| Kind | When (VN calendar) | Who | CTA |
|------|--------------------|-----|-----|
| `d1` | The calendar day after the user's **first check-in** | Active, has not checked in that day. Once | `…/check-in?src=email_d1` |
| `d3` | Three calendar days after the first check-in | Did **not** check in on day 2 (first check-in + 2 days), and has not checked in on the send day. Once | `…/check-in?src=email_d3` |

Day 0 is the first check-in. Day 2 is two days later. A check-in on day 2 suppresses the Day-3 email.

- ≤1 email per kind per user (`email_send_receipts` claim-before-send). Successful sends store Resend's email id on the receipt.
- Skip if `checked_in_today`, inactive, invalid address, `email_unsubscribed_at` is set, or the address is marked undeliverable (`email_reminder_suppressed_at`). Immediate suppression only when Resend's body says the recipient `to` address is invalid. Other 4xx, including 400/422 that blame `from` or the payload, count toward 3 failures unless that same error hits many users in one run (treated as a sender problem and not counted). Network errors, HTTP 5xx, 408, 429, and 401/403 still release the receipt and retry. Changing `users.email` clears the suppression on the next run.
- D0 CTA stays `https://dadiary.vn/check-in` (or `DADIARY_PUBLIC_WEB_URL` + `/check-in`) with no `src`. D1/D3 append `src` as in the table. No magic-link auth exists; the web app is auth-aware if the session cookie is present.
- Opens and clicks: `POST /api/v1/email/resend/webhook` (Svix signature, `DADIARY_RESEND_WEBHOOK_SECRET`). Stores `email.opened` and `email.clicked` in `email_engagement_events`, joined to the user via `email_send_receipts.resend_email_id`. Other event types are HTTP 200 no-ops. Duplicate Svix ids do not insert a second row.
- Push permission card re-show: `GET` / `PUT /api/v1/me/reminder`. `skip_push_opt_in` records the first skip. `push_opt_in_reshow_eligible` becomes true on the VN civil day 3 days later, until `consume_push_opt_in_reshow`. The client must show that card only immediately after a successful check-in.
- Push click: `POST /api/v1/me/push/click` (JWT). The service worker `notificationclick` handler should call this (the page can relay it with the access token). Body: `kind`, `tag`, `idempotency_key`, `clicked_at`.
- Unsubscribe: `GET|POST /api/v1/email/unsubscribe?token=…` (HMAC with `DADIARY_JWT_SECRET`). `List-Unsubscribe` header is set when `DADIARY_PUBLIC_API_URL` is present.
- Vietnamese copy only; soft DaDiary tone; no diagnosis or cure claims.
- **Missing ESP:** the path no-ops and logs `email no-op — ESP not configured (set RESEND_API_KEY and EMAIL_FROM)`.

Railway setup (do not invent keys):

```
RESEND_API_KEY=re_...
EMAIL_FROM=DaDiary <noreply@your-verified-domain>
DADIARY_PUBLIC_API_URL=https://<your-api-host>
DADIARY_PUBLIC_WEB_URL=https://dadiary.vn
DADIARY_RESEND_WEBHOOK_SECRET=whsec_...
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
| `DADIARY_RESEND_WEBHOOK_SECRET` / `RESEND_WEBHOOK_SECRET` | empty | Svix signing secret (`whsec_…`) for `POST /api/v1/email/resend/webhook`. Empty → endpoint returns 503 |
| `DADIARY_PENDING_ORDER_EXPIRY_ENABLED` | true | Boot + daily pending expire |
| `DADIARY_PENDING_ORDER_TTL_HOURS` | 72 | Local pending TTL (24–168) |

Schema: `018_email_receipts_and_unsub`, plus `024_reminder_engagement` (`resend_email_id`, `email_engagement_events`, push opt-in columns, `push_click_events`). Do not invent `RESEND_API_KEY` or the webhook secret.
