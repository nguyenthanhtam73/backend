# Founder funnel health (`GET /api/v1/admin/funnel-stats`)

Read-only Postgres proxies for the leaky bucket. Admin JWT only
(`DADIARY_ADMIN_EMAILS` / `user.is_admin`). Does not write. Paywall views are
client-only and are returned as `null` with a note.

## Response (`data`)

| Field | Meaning |
|-------|---------|
| `signed_up_1d` / `signed_up_7d` | Users created in a rolling 24h / 7d window |
| `skin_check_users_ever` | Distinct users with ≥1 `skin_checks` row |
| `skin_check_users_1d` / `_7d` | Distinct users with a check **created** in that rolling window |
| `d0_checkin_users` | Users with `check_date` on their Vietnam signup day |
| `d0_checkin_users_7d` | Same, among users who signed up in the last 7d |
| `d1_checkin_users` | Users with `check_date` on the Vietnam day after signup |
| `d1_eligible_users` | Users whose signup VN day is **before** today (D1 has happened) |
| `d1_checkin_users_7d` / `d1_eligible_users_7d` | Same proxies among 7d signups |
| `paid_orders_7d` | `payment_orders` with `status=paid` and `COALESCE(paid_at, created_at)` in last 7d |
| `paywall_views` | Always `null` |
| `notes` | Calendar + paywall + D0/D1 definitions |
| `as_of` | UTC timestamp when counts were computed |

Calendar for D0/D1 is `Asia/Ho_Chi_Minh` (same as `streaktime` / `skin_checks.check_date`).

## Curl verify

```bash
# 1) Sign in as an admin email listed in DADIARY_ADMIN_EMAILS
TOKEN=$(curl -sS -X POST https://<api>/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"<admin-email>","password":"<password>"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["access_token"])')

# 2) Funnel strip
curl -sS https://<api>/api/v1/admin/funnel-stats \
  -H "Authorization: Bearer $TOKEN" | python3 -m json.tool
```

Expect HTTP 200 and:

```json
{
  "success": true,
  "data": {
    "signed_up_1d": 0,
    "signed_up_7d": 0,
    "skin_check_users_ever": 0,
    "skin_check_users_1d": 0,
    "skin_check_users_7d": 0,
    "d0_checkin_users": 0,
    "d0_checkin_users_7d": 0,
    "d1_checkin_users": 0,
    "d1_eligible_users": 0,
    "d1_checkin_users_7d": 0,
    "d1_eligible_users_7d": 0,
    "paid_orders_7d": 0,
    "paywall_views": null,
    "notes": {
      "paywall": "N/A — paywall is client-only and is not persisted in Postgres",
      "calendar": "Asia/Ho_Chi_Minh"
    },
    "as_of": "2026-09-05T12:00:00Z"
  }
}
```

Auth checks:

```bash
# no token → 401
curl -sS -o /tmp/funnel-noauth.json -w '%{http_code}\n' \
  https://<api>/api/v1/admin/funnel-stats

# non-admin JWT → 403
curl -sS -o /tmp/funnel-forbidden.json -w '%{http_code}\n' \
  https://<api>/api/v1/admin/funnel-stats \
  -H "Authorization: Bearer <non-admin-access-token>"
```

SQL spot-check (optional, production replica / `railway connect`):

```sql
-- signed up 7d
SELECT count(*) FROM users
WHERE deleted_at IS NULL
  AND created_at >= now() - interval '7 days';

-- distinct users with a skin_check
SELECT count(DISTINCT user_id) FROM skin_checks
WHERE deleted_at IS NULL;

-- paid orders 7d
SELECT count(*) FROM payment_orders
WHERE deleted_at IS NULL
  AND status = 'paid'
  AND COALESCE(paid_at, created_at) >= now() - interval '7 days';
```

Frontend page (separate repo: `nguyenthanhtam73/frontend`):

- Route: `/admin/funnel` (admin JWT / `user.is_admin` only)
- Client: `GET /api/v1/admin/funnel-stats` via the existing `apiGet` helper
- Render: compact cards for signup 1d/7d, skin-check users ever/1d/7d, D0, D1 (`n / eligible`), paid 7d, paywall **N/A**
- Nav: admin header chip + links from `/admin/activity` and `/admin/payments`

This agent could not open the frontend PR (GitHub token is scoped to this backend repo). Apply the page there, or re-run a frontend-scoped agent against the contract above.
