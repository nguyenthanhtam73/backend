# D0/D1 check-in reminders + stale SePay pending orders

## What we found

| Path | Exists today | Used for |
|------|----------------|----------|
| Evening Web Push (`DailyReminderJob`, 20:00 VN) | Yes, if VAPID keys + user subscribed | Anyone who has not checked in *today* (and is not streak-at-risk) |
| Streak-at-risk push | Yes | Days-since 1 / savable 2 |
| Outbound email / ESP | **No** | There is no SMTP/SendGrid/Resend client. `email` on users is an identifier only |
| SePay checkout | Yes | Creates `payment_orders` as `pending`; IPN marks `paid` |
| Pending-order cleanup | **Was missing** | Historical unpaid checkouts stayed `pending` forever |

We did **not** add an email provider. The app path is flags + an API the frontend can poll.

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
      "email": false,
      "push_evening": true,
      "push_d0_d1_specific": false,
      "email_reason": "no_outbound_email",
      "push_note": "evening_daily_reminder_exists_not_d0_d1_specific"
    }
  }
}
```

Show an in-app nudge when `due` is true. Do not treat `kind=none` as an error.

### What’s still missing for email / push

- **Email:** no ESP, no templates, no unsubscribe. Do not set `channels.email` until a provider is wired.
- **D0/D1-specific push:** not sent. Users who already granted Web Push still get the existing 20:00 VN `daily_reminder` if they have not checked in. A second D0/D1 push the same evening would double-nudge.

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
calendar), and paid orders in the last 7 days. Paywall impressions are
client-only (`paywall_views` is `null`). Curl + field table:
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
| `DADIARY_CHECKIN_REMINDER_ENABLED` | true | Boot + daily flag refresh |
| `DADIARY_PENDING_ORDER_EXPIRY_ENABLED` | true | Boot + daily pending expire |
| `DADIARY_PENDING_ORDER_TTL_HOURS` | 72 | Local pending TTL (24–168) |
