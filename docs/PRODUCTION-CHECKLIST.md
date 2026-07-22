# DaDiary Production Go-Live Checklist

Use this before taking real money on SePay. Sandbox deploy steps live in
[`SEPAY_DEPLOY_CHECKLIST.md`](./SEPAY_DEPLOY_CHECKLIST.md).

## 1. SePay production keys + IPN

- [ ] SePay dashboard switched to **production** merchant
- [ ] `DADIARY_SEPAY_ENV=production`
- [ ] `DADIARY_SEPAY_MERCHANT_ID` = live merchant id (not `SP-TEST-…`)
- [ ] `DADIARY_SEPAY_SECRET_KEY` = live secret (not sandbox `spsk_test_…`)
- [ ] IPN URL (SECRET_KEY auth):
  ```
  POST https://<your-api-host>/api/v1/payment/sepay/webhook
  ```
- [ ] IPN secret matches `DADIARY_SEPAY_SECRET_KEY` exactly
- [ ] SePay “test send” / probe returns **401** without secret, **200** with valid paid payload
- [ ] Callback URLs point at production web:
  - `DADIARY_PUBLIC_WEB_URL=https://dadiary.vn`
  - success / error / cancel under `/payment/*` (locale prefixes OK)

## 2. Env production (no sandbox secrets)

- [ ] `DADIARY_ENV=production` (or equivalent host env)
- [ ] Strong `DADIARY_JWT_SECRET` (rotate if it was ever shared / committed)
- [ ] `DADIARY_ADMIN_EMAILS` set to real operator emails only
- [ ] `DADIARY_E2E_SECRET` **empty / unset** on production (no force-plan helpers)
- [ ] OpenAI / Anthropic / VAPID / Turnstile keys are production values
- [ ] Confirm Railway (or host) env has **no** leftover sandbox SePay defaults
- [ ] Restart API after env changes

## 3. HTTPS + domain

- [ ] API served only over HTTPS (Railway / reverse proxy terminates TLS)
- [ ] Web app on production domain (`https://dadiary.vn`) with valid cert
- [ ] `NEXT_PUBLIC_API_URL` points at the production API host
- [ ] CORS / allowed origins reviewed for production traffic
- [ ] Health check: `GET /api/v1/health` → ok

## 4. Rate limit + security headers

- [ ] AI / expensive routes still behind JWT + per-user rate limiters
  (`internal/middleware/ratelimit.go`)
- [ ] Edge / reverse proxy rate limits enabled for public routes
  (especially `POST /payment/sepay/webhook` and `/auth/*`)
- [ ] Security headers at edge or app (recommended):
  - `Strict-Transport-Security`
  - `X-Content-Type-Options: nosniff`
  - `X-Frame-Options: DENY` (or CSP `frame-ancestors`)
  - `Referrer-Policy: strict-origin-when-cross-origin`
- [ ] Panic recovery + request IDs enabled (`middleware.RegisterDefault`)
- [ ] Webhook auth: invalid `X-Secret-Key` must not mutate plans (expect 401 + alert)

## 5. Backup DB

- [ ] Automated Postgres backups enabled (Railway backup / pg_dump cron / managed PITR)
- [ ] Restore drill documented (who runs it, RPO/RTO)
- [ ] Confirm tables exist: `payment_orders`, `subscriptions`, `plan_change_logs`,
      `payment_ops_events` (migration `009` or AutoMigrate)
- [ ] Snapshot taken immediately before first live checkout

## 6. Monitoring alerts enabled

- [ ] `DADIARY_ALERT_WEBHOOK_URL` (Slack) and/or Telegram bot+chat configured
- [ ] Alert sink received a test event (restart + intentional bad IPN secret →
      `SePay webhook: signature_invalid`)
- [ ] Fail-rate monitor active (fail rate > 10% / hour or webhook errors > 5 / hour)
- [ ] `PlanExpiryJob` wired with same alerter (job fail or downgrade > 20 users)
- [ ] Admin metrics reachable (admin JWT):
  ```
  GET /api/v1/admin/metrics/payment
  ```
  Expect: `today_payments`, `success_rate`, `total_revenue`, `failed_count`,
  `webhook_errors_last_24h`, `active_premium_count`, `upcoming_expiries`

## 7. Smoke test production

Run with a **small real payment** (or SePay production test path if offered):

- [ ] Logged-in user → Pricing → checkout → SePay → return `/payment/success`
- [ ] `GET /me` shows paid `plan_tier` + expiry
- [ ] `payment_orders.status = paid` for the invoice
- [ ] IPN log line `payment: fulfill success` present
- [ ] Cancel flow: `POST /subscription/cancel` → access until period end
- [ ] Admin metrics reflects the paid order / revenue
- [ ] Bad IPN secret produces Error log + ops alert (no plan change)
- [ ] Frontend EN/VI success & error pages load on production domain

## Sign-off

| Role | Name | Date | Notes |
|------|------|------|-------|
| Backend | | | |
| Product / Ops | | | |

When all boxes above are checked, the stack is ready for live Premium checkout.
