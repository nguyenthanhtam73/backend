# Founder funnel health (`GET /api/v1/admin/funnel-stats`)

Read-only Postgres proxies for the leaky bucket. Admin JWT only
(`DADIARY_ADMIN_EMAILS` / `user.is_admin`). Does not write.

Paywall impressions are persisted when the client POSTs
`/api/v1/analytics/paywall-view` (JWT optional). Counts are never `null`
(0 before any traffic).

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
| `paywall_views_1d` / `paywall_views_7d` | Rows in `paywall_views` created in that rolling window |
| `paywall_views` | Same as `paywall_views_7d` (legacy single field; never `null`) |
| `notes` | Calendar + paywall + D0/D1 definitions |
| `as_of` | UTC timestamp when counts were computed |

Calendar for D0/D1 is `Asia/Ho_Chi_Minh` (same as `streaktime` / `skin_checks.check_date`).

## Persist paywall impressions

```
POST /api/v1/analytics/paywall-view
Authorization: Bearer <access>   # optional — guests still count
Content-Type: application/json

{
  "surface": "upsell_banner" | "pricing" | "upgrade",
  "feature": "wardrobe_full",          // optional, default "generic"
  "recommended_plan": "premium"        // optional
}
```

Returns `201` `{ "success": true, "data": { "id": "…", "logged_at": "…" } }`.
Rate-limited (40 / 15 min per user or IP). Fire-and-forget from the client —
do not toast failures, do not skip existing Meta / `dataLayer` `paywall_view`
events.

Does **not** change SePay keys or gateway mode.

## Curl verify

```bash
# 1) Sign in as an admin email listed in DADIARY_ADMIN_EMAILS
TOKEN=$(curl -sS -X POST https://<api>/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"<admin-email>","password":"<password>"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["access_token"])')

# 2) Record an impression (any user or no JWT)
curl -sS -X POST https://<api>/api/v1/analytics/paywall-view \
  -H 'Content-Type: application/json' \
  -d '{"surface":"pricing","feature":"generic"}' | python3 -m json.tool

# 3) Funnel strip
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
    "paywall_views_1d": 1,
    "paywall_views_7d": 1,
    "paywall_views": 1,
    "notes": {
      "paywall": "Impression rows from POST /api/v1/analytics/paywall-view (upsell_banner, pricing, upgrade). Rolling 1d/7d from as_of. paywall_views equals paywall_views_7d.",
      "calendar": "Asia/Ho_Chi_Minh"
    },
    "as_of": "2026-09-06T12:00:00Z"
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

-- paywall impressions 1d / 7d
SELECT
  count(*) FILTER (WHERE created_at >= now() - interval '1 day')  AS paywall_views_1d,
  count(*) FILTER (WHERE created_at >= now() - interval '7 days') AS paywall_views_7d
FROM paywall_views;
```

## Frontend hook (`nguyenthanhtam73/frontend`)

Client already fires Meta / `dataLayer` `paywall_view` from:

- `components/premium/upsell-banner.tsx` (`PaywallViewTracker`)
- `components/pricing/pricing-view.tsx` (when arriving with an upsell feature)

Keep those Meta calls. After `trackFunnelEventOnce` returns `true`, POST the
same payload (do not toast; do not clear the session on 401):

```ts
// lib/api/paywall-view.ts
import { apiPost } from "@/lib/api-client";

export async function persistPaywallView(input: {
  surface: string;
  feature?: string;
  recommendedPlan?: string;
}): Promise<void> {
  try {
    await apiPost(
      "/api/v1/analytics/paywall-view",
      {
        surface: input.surface,
        feature: input.feature,
        recommended_plan: input.recommendedPlan,
      },
      { toastOnError: false, clearTokenOn401: false, timeoutMs: 5000 },
    );
  } catch {
    /* swallow — best-effort, same as affiliate/clicks */
  }
}
```

```ts
// lib/analytics/funnel.ts — add next to trackFunnelEventOnce
export function reportPaywallView(
  input: {
    surface: PaywallSurface;
    feature?: string | null;
    recommendedPlan?: string | null;
  },
  scope = "",
): boolean {
  if (typeof window === "undefined") return false;
  const params = paywallViewParams(input);
  const first = trackFunnelEventOnce(FUNNEL_EVENTS.paywallView, params, scope);
  if (first) {
    void persistPaywallView({
      surface: input.surface,
      feature: typeof params.feature === "string" ? params.feature : undefined,
      recommendedPlan: input.recommendedPlan ?? undefined,
    });
  }
  return first;
}
```

Replace `trackFunnelEventOnce(FUNNEL_EVENTS.paywallView, …)` in the two
surfaces above with `reportPaywallView(…)`. On the pricing page, also report
when there is no `upsellFrom` (`feature: "generic"`, scope `pricing:page`) so
organic `/pricing` visits count.

Admin `/admin/funnel`:

- Types: add `paywall_views_1d` / `paywall_views_7d` (`number`); keep
  `paywall_views` as the 7d count (no longer `null`).
- Cards: show 1d and 7d the same way as sign-ups. Stop treating null as N/A
  once the backend is deployed (`0` is a real count).

Frontend page (this repo's companion):

- Route: `/admin/funnel` (admin JWT / `user.is_admin` only)
- Client: `GET /api/v1/admin/funnel-stats` via the existing `apiGet` helper
- Render: compact cards for signup 1d/7d, skin-check users ever/1d/7d, D0, D1
  (`n / eligible`), paid 7d, paywall views 1d/7d
- Nav: admin header chip + links from `/admin/activity` and `/admin/payments`
