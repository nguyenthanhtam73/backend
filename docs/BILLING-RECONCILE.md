# Billing reconcile (subscription state)

## What was wrong (plain language)

When someone paid via SePay, DaDiary wrote two things:

1. The **user** row (`users.plan_tier`, `subscription_status`, `plan_expires_at`) — this is what the app uses for “are they Premium?”
2. A **subscription history** row (`subscriptions.status = active`, `period_ends_at`)

When the paid month ended, a daily job updated the **user** (back to Free / Expired) but left the **subscription** row marked `active`. So the database disagreed with itself:

- 15 subscription rows still said “active” even though every `period_ends_at` was in the past
- 13 of those people were already Free/Expired on `users`
- 2 people still looked Premium on `users` while their billed period had already ended (one still inside the 3-day grace window; one looked like a lifetime grant with no expiry date)

Feature gates already treat a user as Free after expiry + grace, so most people were not getting free Premium. The bug is **incorrect billing records**, which breaks admin counts and any check that trusts `subscriptions.status = 'active'`.

## What we changed

- Expiry / renew / cancel now **close** leftover open subscription rows. A row cannot stay `active` after `period_ends_at`.
- A safe, **idempotent** reconcile (safe to run twice):
  - expires overdue `subscriptions` (or marks them `past_due` during the 3-day grace window)
  - syncs `users.plan_tier` / `subscription_status` / `plan_expires_at`
  - does **not** revoke admin lifetime grants (Premium + empty `plan_expires_at`)
- The API runs this once on boot, and the daily expiry job runs it every UTC day.
- Operators can also run it by command or admin API.

We did **not** change prices or plan packaging.

## How to repair production (Railway)

After this code is deployed, **restart the backend**. Boot reconcile should close the 15 stale rows automatically. Then verify with the SQL below.

If you want to run it by hand (recommended first as dry-run):

```bash
# From the backend repo, using Railway’s env (includes DADIARY_DATABASE_URL)
railway run --service backend go run ./cmd/reconcile-subscriptions
railway run --service backend go run ./cmd/reconcile-subscriptions --apply
```

Or, logged in as a full admin:

```bash
curl -X POST https://<your-api-host>/api/v1/admin/billing/reconcile \
  -H "Authorization: Bearer <admin-jwt>"
```

### Verify (Postgres)

These two counts should match after repair. They **exclude** lifetime admin grants (NULL expiry) and people who are only in the grace window.

```sql
SELECT count(*) AS active_billed_subscriptions
FROM subscriptions
WHERE status = 'active'
  AND period_ends_at IS NOT NULL
  AND period_ends_at > now();

SELECT count(*) AS active_billed_users
FROM users
WHERE plan_tier IN ('premium', 'premium_plus')
  AND subscription_status = 'active'
  AND plan_expires_at IS NOT NULL
  AND plan_expires_at > now();
```

Leftover overdue actives should be **0**:

```sql
SELECT count(*) AS overdue_still_active
FROM subscriptions
WHERE status = 'active'
  AND period_ends_at IS NOT NULL
  AND period_ends_at <= now();
```

### Emergency SQL (only if the Go command cannot run)

Prefer the command above. This SQL only closes overdue subscription rows; it does not sync `users`. It is idempotent and does not delete anything.

```sql
UPDATE subscriptions
SET status = CASE
      WHEN canceled_at IS NOT NULL THEN 'canceled'
      WHEN period_ends_at + interval '3 days' > now() THEN 'past_due'
      ELSE 'expired'
    END,
    updated_at = now()
WHERE deleted_at IS NULL
  AND status IN ('active', 'trialing', 'canceled', 'past_due')
  AND period_ends_at IS NOT NULL
  AND period_ends_at <= now()
  AND status <> CASE
      WHEN canceled_at IS NOT NULL THEN 'canceled'
      WHEN period_ends_at + interval '3 days' > now() THEN 'past_due'
      ELSE 'expired'
    END;
```

Then re-run the two count queries.

## Lifetime grants

Admin `PUT /api/v1/admin/users/:id/plan` writes Premium with **no** `plan_expires_at`. Reconcile leaves those users Premium. If someone should not be lifetime, revoke them in the admin user plan UI.
