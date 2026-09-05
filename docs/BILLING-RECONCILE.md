# Billing reconcile (subscription state)

## What was wrong (plain language)

When someone paid via SePay, DaDiary wrote two things:

1. The **user** row (`users.plan_tier`, `subscription_status`, `plan_expires_at`) — this is what the app uses for “are they Premium?”
2. A **subscription history** row (`subscriptions.status = active`, `period_ends_at`)

Two kinds of leftover disagreement showed up in production:

**A. Zombie subscription rows** (first repair): the paid month ended, the daily job updated the **user** (back to Free / Expired) but left the **subscription** row marked `active`.

**B. Orphan Premium on `users`** (this repair): after those history rows were closed, some people still looked Premium on `users` while **no** `subscriptions` row had `status=active AND period_ends_at > now()`. Admin `active_premium_count` and `/me` still read `users.plan_tier`, so the app showed Premium.

Typical leftover shapes:

- Dated Premium (`plan_expires_at` still in the future) with no covering history row and no covering paid order — e2e ForcePlan, legacy SePay fulfill, or a user column that was never cleared
- `subscription_status=active` on a Free user
- A free user who still has a covering history row (converse: refresh `users` from that row)

Feature gates already treat a user as Free after expiry + grace. The bug is **incorrect billing records**, which breaks admin counts and any check that trusts `users.plan_tier` alone.

## What we changed

- Expiry / renew / cancel now **close** leftover open subscription rows. A row cannot stay `active` after `period_ends_at`.
- A safe, **idempotent** reconcile (safe to run twice):
  - expires overdue `subscriptions` (or marks them `past_due` during the 3-day grace window)
  - selects **paid-looking users** (paid `plan_tier` or an open `subscription_status`), not only people whose `plan_expires_at` has already passed
  - syncs `users.plan_tier` / `subscription_status` / `plan_expires_at` from verified evidence, in this order:
    1. a covering `subscriptions` row (`period_ends_at` still in the billed window or grace) — source of truth when present
    2. stacked paid `payment_orders` (same `ComputePlanExpiry` fold as SePay renew)
    3. admin lifetime grant (paid + NULL `plan_expires_at`) — left alone
    4. otherwise set the user back to free / expired
  - does **not** invent a new `subscriptions` row from a paid order (history stays what was actually written)
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

`users.plan_tier` premium and `users.subscription_status = active` may still be higher than the billed-period counts when lifetime grants exist:

```sql
SELECT count(*) AS lifetime_grants
FROM users
WHERE plan_tier IN ('premium', 'premium_plus')
  AND plan_expires_at IS NULL;
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
